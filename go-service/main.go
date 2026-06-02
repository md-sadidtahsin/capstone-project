package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	_ "modernc.org/sqlite"
	"github.com/redis/go-redis/v9"
)

var db *sql.DB
var rdb *redis.Client
var ctx = context.Background()
var baseURL string

// pythonServiceURL can be overridden by PYTHON_SERVICE_URL environment variable (kept for backward compatibility)
var pythonServiceURL = getEnv("PYTHON_SERVICE_URL", "http://localhost:5000")

type ShortenRequest struct {
	LongURL string `json:"long_url" binding:"required"`
}

type ShortenResponse struct {
	ShortCode string `json:"short_code"`
	ShortURL  string `json:"short_url"`
	LongURL   string `json:"long_url"`
}

type ClickEvent struct {
	ShortCode string `json:"short_code"`
	ClickedAt string `json:"clicked_at"`
}

func initURL() {
    baseURL = os.Getenv("BASE_URL")
    if baseURL == "" {
        baseURL = "http://localhost:8000" // fallback for local dev
    }
}

func initDB() {
	var err error
	db, err = sql.Open("sqlite", "./go.db")
	if err != nil {
		log.Fatal(err)
	}

	createTableSQL := `CREATE TABLE IF NOT EXISTS urls (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		short_code TEXT UNIQUE NOT NULL,
		long_url TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	_, err = db.Exec(createTableSQL)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Database initialized successfully")
}

func initRedis() {
	redisURL := getEnv("REDIS_URL", "localhost:6380")
	
	rdb = redis.NewClient(&redis.Options{
		Addr:     redisURL,
		Password: "", // no password
		DB:       0,  // default DB
	})

	// Test connection
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		log.Printf("Warning: Redis connection failed: %v. Events will not be published.", err)
		rdb = nil
	} else {
		log.Printf("Redis connected successfully at %s", redisURL)
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

var generateShortCode = func() string {
	b := make([]byte, 6)
	rand.Read(b)
	encoded := base64.URLEncoding.EncodeToString(b)
	// Take first 6 characters and remove any special chars
	shortCode := encoded[:6]
	return shortCode
}

func createShortURL(c *gin.Context) {
	var req ShortenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	shortCode := generateShortCode()

	// Check if short code already exists (unlikely but possible)
	var exists int
	err := db.QueryRow("SELECT COUNT(*) FROM urls WHERE short_code = ?", shortCode).Scan(&exists)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	// Regenerate if exists (very rare)
	for exists > 0 {
		shortCode = generateShortCode()
		db.QueryRow("SELECT COUNT(*) FROM urls WHERE short_code = ?", shortCode).Scan(&exists)
	}

	_, err = db.Exec("INSERT INTO urls (short_code, long_url) VALUES (?, ?)", shortCode, req.LongURL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create short URL"})
		return
	}

	response := ShortenResponse{
		ShortCode: shortCode,
    	ShortURL:  baseURL + "/api/go/" + shortCode,  // ✅ Now configurable!
    	LongURL:   req.LongURL,
	}

	log.Printf("Created short URL: %s -> %s", shortCode, req.LongURL)
	c.JSON(http.StatusOK, response)
}

func redirect(c *gin.Context) {
	shortCode := c.Param("code")
	var longURL string

	// Try Redis cache first (if available)
	if rdb != nil {
		cachedURL, err := rdb.Get(ctx, "url:"+shortCode).Result()
		if err == nil {
			log.Printf("Cache hit for %s", shortCode)
			longURL = cachedURL
			// Publish click event to Redis
			go publishClickEvent(shortCode)
			c.Redirect(http.StatusMovedPermanently, longURL)
			return
		}
	}

	// Cache miss or Redis unavailable - query database
	err := db.QueryRow("SELECT long_url FROM urls WHERE short_code = ?", shortCode).Scan(&longURL)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Short URL not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	// Cache the URL in Redis (1 hour TTL)
	if rdb != nil {
		rdb.Set(ctx, "url:"+shortCode, longURL, 1*time.Hour)
		log.Printf("Cached URL for %s", shortCode)
	}

	// Publish click event to Redis (or fallback to HTTP)
	go publishClickEvent(shortCode)

	// Redirect to the long URL
	c.Redirect(http.StatusMovedPermanently, longURL)
}

func publishClickEvent(shortCode string) {
	event := ClickEvent{
		ShortCode: shortCode,
		ClickedAt: time.Now().Format(time.RFC3339),
	}

	// Try Redis Pub/Sub first
	if rdb != nil {
		jsonData, err := json.Marshal(event)
		if err != nil {
			log.Printf("Error marshaling event: %v", err)
			return
		}

		err = rdb.Publish(ctx, "click_events", jsonData).Err()
		if err != nil {
			log.Printf("Redis publish error: %v, falling back to HTTP", err)
			// Fallback to HTTP if Redis fails
			sendClickEventHTTP(shortCode)
		} else {
			log.Printf("✅ Click event published to Redis: %s", shortCode)
		}
	} else {
		// No Redis available, use HTTP fallback
		sendClickEventHTTP(shortCode)
	}
}

func sendClickEventHTTP(shortCode string) {
	event := ClickEvent{
		ShortCode: shortCode,
		ClickedAt: time.Now().Format(time.RFC3339),
	}

	jsonData, err := json.Marshal(event)
	if err != nil {
		log.Printf("Error marshaling event: %v", err)
		return
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Post(pythonServiceURL+"/api/events", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("Error sending event to Python service: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Python service returned status: %d", resp.StatusCode)
	} else {
		log.Printf("Click event sent via HTTP for: %s", shortCode)
	}
}

func main() {
	initURL()

	initDB()
	defer db.Close()

	initRedis()
	if rdb != nil {
		defer rdb.Close()
	}
	r := setupRouter()
	log.Println("Go service starting on :8000")
	r.Run(":8000")
}

// setupRouter builds and returns the Gin router with middleware and routes.
func setupRouter() *gin.Engine {
	r := gin.Default()

	// CORS middleware
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusOK)
			return
		}

		c.Next()
	})

	// Routes
	r.POST("/api/shorten", createShortURL)
	r.GET("/:code", redirect)

	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "Go service root"})
	})

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":    "healthy",
			"service":   "go-shortener-service",
			"timestamp": time.Now().Format(time.RFC3339),
		})
	})

	return r
}
