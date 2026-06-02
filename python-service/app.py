from flask import Flask, render_template, request, jsonify
import sqlite3
import requests
from datetime import datetime, timedelta, timezone
import logging
import os
import redis
import json
import threading

app = Flask(__name__)
logging.basicConfig(level=logging.INFO)

# Use environment variables for Docker, fallback to localhost for local dev
GO_SERVICE_URL = os.getenv("GO_SERVICE_URL", "http://localhost:8000")
NODE_SERVICE_URL = os.getenv("NODE_SERVICE_URL", "http://localhost:3000")
REDIS_URL = os.getenv("REDIS_URL", "localhost:6380")
DATABASE = os.getenv("DATABASE", "python.db")

# Initialize Redis client
redis_client = None


def init_redis():
    """Initialize Redis connection"""
    global redis_client
    try:
        # Parse host and port
        host_port = REDIS_URL.split(":")
        host = host_port[0]
        port = int(host_port[1]) if len(host_port) > 1 else 6379

        redis_client = redis.Redis(host=host, port=port, decode_responses=True)
        redis_client.ping()
        logging.info(f"✅ Redis connected successfully at {REDIS_URL}")

        # Start Redis subscriber in background thread
        subscriber_thread = threading.Thread(target=redis_subscriber, daemon=True)
        subscriber_thread.start()
        logging.info("Redis subscriber thread started")

    except Exception as e:
        logging.warning(f"Redis connection failed: {e}. Will use HTTP endpoint only.")
        redis_client = None


def redis_subscriber():
    """Subscribe to Redis click_events channel"""
    try:
        pubsub = redis_client.pubsub()
        pubsub.subscribe("click_events")
        logging.info("📡 Subscribed to 'click_events' channel")

        for message in pubsub.listen():
            if message["type"] == "message":
                try:
                    event_data = json.loads(message["data"])
                    process_click_event(event_data)
                except Exception as e:
                    logging.exception(f"Error processing Redis event: {e}")
    except Exception as e:
        logging.exception(f"Redis subscriber error: {e}")


def process_click_event(data):
    """Process click event from Redis or HTTP"""
    short_code = data.get("short_code")
    clicked_at = data.get("clicked_at", datetime.now().isoformat())

    conn = get_db()
    cursor = conn.cursor()

    # Store click event
    cursor.execute(
        """
        INSERT INTO click_events (short_code, clicked_at)
        VALUES (?, ?)
    """,
        (short_code, clicked_at),
    )

    # Update metadata
    cursor.execute(
        """
        UPDATE url_metadata
        SET total_clicks = total_clicks + 1,
            last_clicked = ?
        WHERE short_code = ?
    """,
        (clicked_at, short_code),
    )

    conn.commit()
    conn.close()

    logging.info(f"📊 Processed click event for: {short_code}")


def init_db():
    """Initialize the database with required tables"""
    conn = sqlite3.connect(DATABASE)
    cursor = conn.cursor()

    # Table for storing click events
    cursor.execute(
        """
        CREATE TABLE IF NOT EXISTS click_events (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            short_code TEXT NOT NULL,
            clicked_at DATETIME NOT NULL
        )
    """
    )

    # Table for storing URL metadata
    cursor.execute(
        """
        CREATE TABLE IF NOT EXISTS url_metadata (
            short_code TEXT PRIMARY KEY,
            long_url TEXT NOT NULL,
            total_clicks INTEGER DEFAULT 0,
            first_seen DATETIME NOT NULL,
            last_clicked DATETIME,
            title TEXT,
            description TEXT,
            favicon_url TEXT,
            metadata_status TEXT DEFAULT 'pending'
        )
    """
    )

    conn.commit()
    conn.close()
    logging.info("Database initialized successfully")


def get_db():
    """Get database connection"""
    conn = sqlite3.connect(DATABASE)
    conn.row_factory = sqlite3.Row
    return conn


@app.route("/", methods=['GET'])
def dashboard():
    """Main dashboard page"""
    return render_template("dashboard.html")


@app.route("/create", methods=["POST"])
def create_short_url():
    """Create a new short URL by calling Go service"""
    long_url = request.form.get("long_url")

    if not long_url:
        return jsonify({"error": "URL is required"}), 400

    try:
        # Call Go service to create short URL
        response = requests.post(
            f"{GO_SERVICE_URL}/api/shorten", json={"long_url": long_url}, timeout=5
        )

        if response.status_code == 200:
            data = response.json()

            # Call Node.js service to fetch metadata asynchronously
            metadata = {"status": "unavailable"}
            try:
                node_response = requests.post(
                    f"{NODE_SERVICE_URL}/api/metadata",
                    json={"short_code": data["short_code"], "long_url": long_url},
                    timeout=7,
                )
                if node_response.status_code == 200:
                    metadata = node_response.json()
                    logging.info(f"✅ Metadata fetched: {metadata.get('title', 'N/A')}")
                else:
                    logging.warning(
                        f"Node.js service returned status: {node_response.status_code}"
                    )
            except requests.exceptions.RequestException as e:
                logging.warning(f"Node.js service unavailable: {e}")

            # Store metadata in Python database
            conn = get_db()
            cursor = conn.cursor()

            if metadata.get("status") == "success":
                cursor.execute(
                    """
                    INSERT OR IGNORE INTO url_metadata 
                    (short_code, long_url, first_seen, title, description, favicon_url, metadata_status)
                    VALUES (?, ?, ?, ?, ?, ?, 'fetched')
                """,
                    (
                        data["short_code"],
                        long_url,
                        datetime.now().isoformat(),
                        metadata.get("title"),
                        metadata.get("description"),
                        metadata.get("favicon_url"),
                    ),
                )
            else:
                cursor.execute(
                    """
                    INSERT OR IGNORE INTO url_metadata (short_code, long_url, first_seen, metadata_status)
                    VALUES (?, ?, ?, 'failed')
                """,
                    (data["short_code"], long_url, datetime.now().isoformat()),
                )

            conn.commit()
            conn.close()

            # Add metadata to response
            data["metadata"] = metadata

            logging.info(f"Created short URL: {data['short_code']} -> {long_url}")
            return jsonify(data), 200
        else:
            return (
                jsonify({"error": "Failed to create short URL"}),
                response.status_code,
            )

    except requests.exceptions.RequestException as e:
        logging.exception(f"Error calling Go service: {e}")
        return jsonify({"error": "Go service unavailable"}), 503


@app.route("/api/events", methods=["POST"])
def receive_event():
    """Receive click events from Go service (HTTP fallback)"""
    data = request.get_json()

    if not data or "short_code" not in data:
        return jsonify({"error": "Invalid event data"}), 400

    # Process using the same function as Redis subscriber
    process_click_event(data)

    return jsonify({"status": "success"}), 200


@app.route("/api/stats")
def get_stats():
    """Get analytics statistics"""
    conn = get_db()
    cursor = conn.cursor()

    # Total URLs created
    cursor.execute("SELECT COUNT(DISTINCT short_code) FROM url_metadata")
    total_urls = cursor.fetchone()[0]

    # Total clicks
    cursor.execute("SELECT COUNT(*) FROM click_events")
    total_clicks = cursor.fetchone()[0]

    # Top 10 most clicked URLs
    cursor.execute(
        """
        SELECT short_code, long_url, total_clicks, last_clicked, title, description, favicon_url, metadata_status
        FROM url_metadata
        WHERE total_clicks > 0
        ORDER BY total_clicks DESC
        LIMIT 10
    """
    )
    top_urls = [dict(row) for row in cursor.fetchall()]

    # Recent clicks (last 20)
    cursor.execute(
        """
        SELECT ce.short_code, ce.clicked_at, um.long_url
        FROM click_events ce
        LEFT JOIN url_metadata um ON ce.short_code = um.short_code
        ORDER BY ce.clicked_at DESC
        LIMIT 20
    """
    )
    recent_clicks = [dict(row) for row in cursor.fetchall()]

    # Clicks over time (last 24 hours, hourly breakdown)
    twenty_four_hours_ago = (datetime.now() - timedelta(hours=24)).isoformat()
    cursor.execute(
        """
        SELECT 
            strftime('%Y-%m-%d %H:00:00', clicked_at) as hour,
            COUNT(*) as count
        FROM click_events
        WHERE clicked_at >= ?
        GROUP BY hour
        ORDER BY hour
    """,
        (twenty_four_hours_ago,),
    )
    clicks_over_time = [dict(row) for row in cursor.fetchall()]

    # All URLs created
    cursor.execute(
        """
        SELECT short_code, long_url, total_clicks, first_seen, last_clicked, title, description, favicon_url, metadata_status
        FROM url_metadata
        ORDER BY first_seen DESC
    """
    )
    all_urls = [dict(row) for row in cursor.fetchall()]

    conn.close()

    return jsonify(
        {
            "total_urls": total_urls,
            "total_clicks": total_clicks,
            "top_urls": top_urls,
            "recent_clicks": recent_clicks,
            "clicks_over_time": clicks_over_time,
            "all_urls": all_urls,
        }
    )

@app.route('/health')
def health_check():
    return jsonify({
        "status": "healthy",
        "service": "python-metadata-service",
        "timestamp": datetime.now(timezone.utc).isoformat(),
    })

    


if __name__ == "__main__":
    init_db()
    init_redis()
    app.run(host="0.0.0.0", port=5000, debug=False)
