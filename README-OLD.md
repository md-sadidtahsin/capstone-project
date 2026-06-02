# URL Shortener - Microservice Architecture Demo

A production-ready microservice-based URL shortener demonstrating proper service separation with four independent services: Go for high-performance redirects, Python for analytics and dashboard, Node.js for URL metadata enrichment, and Redis for event-driven communication and caching.

## Architecture

This project demonstrates a realistic microservice architecture where different services handle their specific responsibilities:

### Services

**Go Service (Port 8000)**

- **Purpose**: Fast URL redirection and creation
- **Database**: `go.db` (SQLite)
- **Responsibilities**:
  - Generate and store short codes
  - Handle URL redirects with minimal latency
  - Send click events to Python service asynchronously
- **Technology**: Go with Gin framework

**Python Service (Port 5000)**

- **Purpose**: Analytics, data aggregation, and user interface
- **Database**: `python.db` (SQLite)
- **Responsibilities**:
  - Provide web dashboard for URL creation
  - Orchestrate URL creation (call Go) and metadata fetching (call Node.js)
  - Subscribe to Redis click events channel
  - Collect and aggregate click events
  - Display analytics and statistics with metadata
  - Generate visualizations
  - HTTP fallback endpoint for events
- **Technology**: Python with Flask, redis-py

**Node.js Service (Port 3000)**

- **Purpose**: URL metadata enrichment
- **Database**: `node.db` (SQLite)
- **Responsibilities**:
  - Fetch page titles, descriptions, and favicons from URLs
  - Parse HTML content with Cheerio
  - Store and serve metadata via REST API
- **Technology**: Node.js with Express, Axios, Cheerio

### Microservice Communication

**URL Creation (Synchronous):**

```
User → Python Dashboard
         ↓
         ├→ Go Service → Create Short URL → go.db
         └→ Node.js Service → Fetch Metadata → node.db
         ↓
    Display URL + Metadata in UI
```

**Click Events (Event-Driven with Redis):**

```
User clicks → Go Service
                ↓
            1. Check Redis cache
               ├─ Hit: Instant redirect ⚡
               └─ Miss: Query DB → Cache in Redis
                ↓
            2. Publish to Redis "click_events"
                ↓
            Redis Pub/Sub
                ↓
            Python subscribes → Process event → python.db
```

**Communication Patterns:**

- **Python → Go**: HTTP POST (URL creation - needs immediate response)
- **Python → Node.js**: HTTP POST (metadata fetch - synchronous)
- **Go → Redis**: Pub/Sub publish (click events - decoupled)
- **Redis → Python**: Pub/Sub subscribe (click events - async processing)
- **Go → Redis**: Cache (URL lookups - performance)
- **Fallback**: HTTP POST if Redis unavailable
- **No direct database sharing**: Each service owns its data

## Features

- ✅ Create short URLs through web dashboard
- ✅ **Lightning-fast redirects with Redis caching** ⚡
- ✅ **Event-driven architecture with Redis Pub/Sub**
- ✅ **Never lose events** - Redis queues them if Python is down
- ✅ URL metadata enrichment via Node.js (titles, descriptions, favicons)
- ✅ Real-time analytics dashboard
- ✅ Click tracking and history
- ✅ Visual charts for click patterns
- ✅ Top URLs by popularity with page info
- ✅ Recent activity monitoring
- ✅ Auto-refreshing dashboard (every 5 seconds)
- ✅ Visual indicators showing Node.js service status
- ✅ **Graceful degradation** - HTTP fallback if Redis unavailable

## Prerequisites

- **Go**: Version 1.24 or higher
- **Python**: Version 3.14 (or 3.8+)
- **Node.js**: Version 24.11 or higher (with npm)
- **Redis**: Version 7 or higher (for local: localhost:6380)
- **SQLite**: Built-in with Go, Python, and Node.js
- **Docker & Docker Compose**: For containerized deployment (recommended)

## Installation & Setup

### Option 1: Docker (Recommended) 🐳

**Prerequisites:**

- Docker
- Docker Compose

**Quick Start:**

```bash
# Navigate to project
cd /home/xaadu/codes/urlshortner

# Build and start all services
docker-compose up --build

# Or run in background
docker-compose up --build -d
```

**Access the application:**

- Dashboard: `http://localhost:5000`
- Go Service: `http://localhost:8000`
- Node.js Service: `http://localhost:3000`

**Useful Docker Commands:**

```bash
# View logs
docker-compose logs -f

# View logs for specific service
docker-compose logs -f python-service

# Stop all services
docker-compose down

# Stop and remove volumes (deletes databases)
docker-compose down -v

# Rebuild after code changes
docker-compose up --build
```

**How it works:**

- Each service runs in its own container
- Services communicate via Docker network using container names
- Databases persist in Docker volumes
- All services start together with one command!

---

### Option 2: Local Development (Without Docker)

### 1. Clone or navigate to the project

```bash
cd /home/xaadu/codes/urlshortner
```

### 2. Setup Go Service

```bash
cd go-service

# Download dependencies
go mod download

# Run the service
go run main.go
```

The Go service will start on `http://localhost:8000`

### 3. Setup Python Service

Open a new terminal:

```bash
cd /home/xaadu/codes/urlshortner/python-service

# Create virtual environment (following user preference)
python3.14 -m venv venv

# Activate virtual environment
source venv/bin/activate  # On Windows: venv\Scripts\activate

# Install dependencies
pip install -r requirements.txt

# Run the service
python app.py
```

The Python service will start on `http://localhost:5000`

### 4. Setup Redis (Local Development)

```bash
# User has Redis running at localhost:6380
# Services will automatically connect to it
# No additional setup needed!
```

### 5. Setup Node.js Service

Open a new terminal:

```bash
cd /home/xaadu/codes/urlshortner/node-service

# Install dependencies
npm install

# Run the service
node server.js
```

The Node.js service will start on `http://localhost:3000`

---

## Usage

### Access the Dashboard

Open your browser and navigate to:

```
http://localhost:5000
```

### Create a Short URL

1. Enter a long URL in the input field
2. Click "Shorten"
3. Copy the generated short URL

### Test the Redirect

Visit the short URL in your browser:

```
http://localhost:8000/{short_code}
```

You'll be redirected to the original URL, and the click will be tracked in the analytics.

### View Analytics

The dashboard automatically shows:

- Total URLs created
- Total clicks
- **Page metadata (titles, favicons) fetched by Node.js**
- Clicks over time (24-hour chart)
- Top URLs by popularity with page info
- All created URLs with metadata status indicators
- Recent click activity

The dashboard refreshes every 5 seconds automatically.

**Visual Indicators:**

- ✅ Green badge "✓ Node.js" = Metadata successfully fetched
- ❌ Red badge "✗" = Metadata fetch failed
- Favicon icons displayed next to page titles

## API Endpoints

### Go Service (Port 8000)

**Create Short URL**

```bash
POST /api/shorten
Content-Type: application/json

{
  "long_url": "https://example.com/very/long/url"
}

Response:
{
  "short_code": "abc123",
  "short_url": "http://localhost:8000/abc123",
  "long_url": "https://example.com/very/long/url"
}
```

**Redirect**

```bash
GET /{short_code}
# Redirects to the long URL and sends event to Python service
```

### Python Service (Port 5000)

**Dashboard**

```bash
GET /
# Returns the web dashboard
```

**Create URL (from UI)**

```bash
POST /create
Content-Type: application/x-www-form-urlencoded

long_url=https://example.com
```

**Receive Click Event**

```bash
POST /api/events
Content-Type: application/json

{
  "short_code": "abc123",
  "clicked_at": "2025-11-08T12:00:00Z"
}
```

**Get Statistics**

```bash
GET /api/stats

Returns JSON with:
- total_urls
- total_clicks
- top_urls (with metadata)
- recent_clicks
- clicks_over_time
- all_urls (with metadata)
```

### Node.js Service (Port 3000)

**Fetch Metadata**

```bash
POST /api/metadata
Content-Type: application/json

{
  "short_code": "abc123",
  "long_url": "https://example.com"
}

Response:
{
  "short_code": "abc123",
  "url": "https://example.com",
  "title": "Example Domain",
  "description": "Example domain for documentation",
  "favicon_url": "https://example.com/favicon.ico",
  "status": "success"
}
```

**Get Metadata**

```bash
GET /api/metadata/{short_code}
# Returns stored metadata for a short code
```

**Health Check**

```bash
GET /health
# Returns service health status
```

## Database Schema

### Go Service (go.db)

```sql
CREATE TABLE urls (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    short_code TEXT UNIQUE NOT NULL,
    long_url TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### Python Service (python.db)

```sql
CREATE TABLE click_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    short_code TEXT NOT NULL,
    clicked_at DATETIME NOT NULL
);

CREATE TABLE url_metadata (
    short_code TEXT PRIMARY KEY,
    long_url TEXT NOT NULL,
    total_clicks INTEGER DEFAULT 0,
    first_seen DATETIME NOT NULL,
    last_clicked DATETIME,
    title TEXT,
    description TEXT,
    favicon_url TEXT,
    metadata_status TEXT DEFAULT 'pending'
);
```

### Node.js Service (node.db)

```sql
CREATE TABLE metadata (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    short_code TEXT UNIQUE NOT NULL,
    url TEXT NOT NULL,
    title TEXT,
    description TEXT,
    favicon_url TEXT,
    fetched_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

## Microservice Design Principles Demonstrated

1. **Service Independence**: Each service has its own database and can run independently
2. **Single Responsibility**: Go=Redirects, Python=Analytics/UI, Node.js=Metadata, Redis=Messaging
3. **Event-Driven Architecture**: Redis Pub/Sub for decoupled async communication
4. **API Communication**: Services communicate via REST APIs for synchronous operations
5. **Service Orchestration**: Python orchestrates calls to both Go and Node.js
6. **Message Broker**: Redis as central message bus (industry-standard pattern)
7. **Caching Strategy**: Redis caching layer for performance optimization
8. **Graceful Degradation**: System works even if Redis or Node.js unavailable
9. **Data Ownership**: Each service owns and manages its own data
10. **Scalability**: Services can be scaled independently, Redis enables horizontal scaling
11. **Containerization**: Each service runs in isolated Docker containers
12. **Environment Configuration**: Services use environment variables for Docker/local flexibility
13. **Resilience**: Events never lost - queued in Redis until processed

## Testing the System

### Docker Testing

If you're running with Docker:

```bash
# Start services
docker-compose up --build

# In another terminal, test with curl
curl -X POST http://localhost:5000/create -d "long_url=https://github.com"

# Watch logs in real-time
docker-compose logs -f

# View specific service logs
docker-compose logs go-service
docker-compose logs python-service
docker-compose logs node-service
```

### Test URL Creation and Redirection

```bash
# Create a short URL
curl -X POST http://localhost:5000/create \
  -d "long_url=https://github.com"

# Test redirect (will open in browser)
curl -L http://localhost:8000/{returned_short_code}

# Check analytics
curl http://localhost:5000/api/stats
```

### Verify Microservice Communication

1. Create a URL through the Python dashboard (e.g., https://github.com)
2. Check Go service logs - you should see the URL creation
3. Check Node.js service logs - you should see metadata fetching
4. Check Python service logs - you should see metadata stored
5. Look at the dashboard - you should see the page title and favicon
6. Click the short URL
7. Check Go service logs - you should see the redirect and event sending
8. Check Python service logs - you should see the click event received
9. Refresh the dashboard - you should see updated analytics with metadata

**Testing Node.js Service Separately:**

```bash
# Test metadata fetching directly
curl -X POST http://localhost:3000/api/metadata \
  -H "Content-Type: application/json" \
  -d '{"short_code":"test123","long_url":"https://github.com"}'

# Check health
curl http://localhost:3000/health
```

## Project Structure

```
/home/xaadu/codes/urlshortner/
├── README.md
├── docker-compose.yml    # Docker Compose with 4 services (includes Redis!)
├── go-service/
│   ├── Dockerfile        # Go container with CGO for SQLite
│   ├── .dockerignore     # Docker ignore file
│   ├── main.go           # Go app with Redis pub/sub & caching
│   ├── go.mod            # Go dependencies (includes go-redis)
│   ├── go.sum            # Go dependency checksums
│   └── go.db             # SQLite database (created at runtime)
├── python-service/
│   ├── Dockerfile        # Python container
│   ├── .dockerignore     # Docker ignore file
│   ├── app.py            # Flask app with Redis subscriber
│   ├── requirements.txt   # Python deps (Flask, requests, redis)
│   ├── python.db         # SQLite database (created at runtime)
│   └── templates/
│       └── dashboard.html # Web dashboard UI with metadata display
└── node-service/
    ├── Dockerfile        # Node.js container
    ├── .dockerignore     # Docker ignore file
    ├── server.js         # Express application (metadata fetching)
    ├── package.json      # Node.js dependencies
    └── node.db           # SQLite database (created at runtime)
```

## Technologies Used

- **Go 1.24**: High-performance backend
  - Gin web framework
  - SQLite3 driver
  - Alpine Linux (Docker base)
- **Python 3.14**: Analytics and UI
  - Flask web framework
  - Requests library
  - SQLite3 (built-in)
  - Slim Debian (Docker base)
- **Node.js 24.11**: Metadata service
  - Express web framework
  - Axios (HTTP client)
  - Cheerio (HTML parsing)
  - SQLite3 driver
  - Alpine Linux (Docker base)
- **Redis 7**: Message broker and cache
  - Pub/Sub for event-driven architecture
  - Caching layer for performance
  - Persistence with AOF (Append-Only File)
- **Docker & Docker Compose**: Containerization and orchestration
- **SQLite**: Lightweight database for all three services
- **Chart.js**: Data visualization
- **Modern CSS**: Responsive dashboard design

## Future Enhancements

- Add Redis for message queue between services
- Implement rate limiting
- Add user authentication
- Support custom short codes
- Add geographic tracking
- Implement URL expiration
- Add bulk URL creation
- Export analytics reports

## Author

[Abdullah Zayed (zayedabdullah.com)](https://zayedabdullah.com)
Contact: [Email (contact@zayedabdullah.com)](mailto:contact@zayedabdullah.com) | [GitHub (xaadu)](https://github.com/xaadu) | [LinkedIn (abdullahzayed01)](https://www.linkedin.com/in/abdullahzayed01/)

## Contributing

Contributions are welcome! Please feel free to submit a pull request.

## Support

If you find this project useful, please consider supporting me with a star or a follow.

## License

MIT License - Free to use for educational purposes
