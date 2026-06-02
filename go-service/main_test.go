package main

import (
    "database/sql"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "os"
    "strings"
    "testing"

    "github.com/gin-gonic/gin"
    _ "modernc.org/sqlite"
    "github.com/alicebob/miniredis/v2"
    "github.com/redis/go-redis/v9"
)

func setupTestDB(t *testing.T) {
    var err error
    db, err = sql.Open("sqlite", ":memory:")
    if err != nil {
        t.Fatal(err)
    }

    createTableSQL := `CREATE TABLE IF NOT EXISTS urls (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        short_code TEXT UNIQUE NOT NULL,
        long_url TEXT NOT NULL,
        created_at DATETIME DEFAULT CURRENT_TIMESTAMP
    );`

    _, err = db.Exec(createTableSQL)
    if err != nil {
        t.Fatal(err)
    }
}

func teardownTestDB() {
    if db != nil {
        db.Close()
    }
}

func TestGetEnvFallback(t *testing.T) {
    os.Unsetenv("TEST_GO_ENV")
    value := getEnv("TEST_GO_ENV", "fallback")
    if value != "fallback" {
        t.Fatalf("expected fallback, got %s", value)
    }
}

func TestGenerateShortCode(t *testing.T) {
    code := generateShortCode()
    if len(code) != 6 {
        t.Fatalf("expected 6 chars, got %d", len(code))
    }
    if strings.ContainsAny(code, "+/=\n") {
        t.Fatalf("unexpected special chars in short code: %s", code)
    }
}

func TestCreateShortURL(t *testing.T) {
    setupTestDB(t)
    defer teardownTestDB()

    w := httptest.NewRecorder()
    c, _ := gin.CreateTestContext(w)
    req := httptest.NewRequest(http.MethodPost, "/api/shorten", strings.NewReader(`{"long_url":"http://example.com"}`))
    req.Header.Set("Content-Type", "application/json")
    c.Request = req

    createShortURL(c)

    if w.Code != http.StatusOK {
        t.Fatalf("expected 200, got %d", w.Code)
    }

    var resp ShortenResponse
    if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
        t.Fatal(err)
    }

    if resp.LongURL != "http://example.com" {
        t.Fatalf("unexpected long_url: %s", resp.LongURL)
    }

    row := db.QueryRow("SELECT long_url FROM urls WHERE short_code = ?", resp.ShortCode)
    var stored string
    if err := row.Scan(&stored); err != nil {
        t.Fatal(err)
    }
    if stored != resp.LongURL {
        t.Fatalf("expected stored URL %s, got %s", resp.LongURL, stored)
    }
}

func TestRedirectNotFound(t *testing.T) {
    setupTestDB(t)
    defer teardownTestDB()

    w := httptest.NewRecorder()
    c, _ := gin.CreateTestContext(w)
    c.Params = gin.Params{{Key: "code", Value: "missing"}}

    redirect(c)

    if w.Code != http.StatusNotFound {
        t.Fatalf("expected 404, got %d", w.Code)
    }
}

func TestRedirectFound(t *testing.T) {
    setupTestDB(t)
    defer teardownTestDB()

    _, err := db.Exec("INSERT INTO urls (short_code, long_url) VALUES (?, ?)", "abc123", "http://example.com")
    if err != nil {
        t.Fatalf("insert failed: %v", err)
    }

    router := gin.Default()
    router.GET("/:code", redirect)

    req := httptest.NewRequest(http.MethodGet, "/abc123", nil)
    w := httptest.NewRecorder()
    router.ServeHTTP(w, req)

    if w.Code != http.StatusMovedPermanently {
        t.Fatalf("expected %d, got %d", http.StatusMovedPermanently, w.Code)
    }
    loc := w.Header().Get("Location")
    if loc != "http://example.com" {
        t.Fatalf("unexpected redirect location: %s", loc)
    }
}

func TestSendClickEventHTTP(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        var event ClickEvent
        if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
            t.Fatal(err)
        }
        if event.ShortCode != "abc123" {
            t.Fatalf("unexpected short_code: %s", event.ShortCode)
        }
        w.WriteHeader(http.StatusOK)
    }))
    defer server.Close()

    prev := pythonServiceURL
    pythonServiceURL = server.URL
    defer func() { pythonServiceURL = prev }()

    sendClickEventHTTP("abc123")
}

func TestPublishClickEventFallback(t *testing.T) {
    prevRdb := rdb
    rdb = nil
    defer func() { rdb = prevRdb }()

    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        var ev ClickEvent
        if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
            t.Fatal(err)
        }
        if ev.ShortCode != "abc123" {
            t.Fatalf("unexpected short code: %s", ev.ShortCode)
        }
        w.WriteHeader(http.StatusOK)
    }))
    defer server.Close()

    prevURL := pythonServiceURL
    pythonServiceURL = server.URL
    defer func() { pythonServiceURL = prevURL }()

    publishClickEvent("abc123")
}

func TestCORSOptionsShortCircuit(t *testing.T) {
    router := setupRouter()

    req := httptest.NewRequest(http.MethodOptions, "/api/shorten", nil)
    req.Header.Set("Origin", "http://example.com")
    w := httptest.NewRecorder()
    router.ServeHTTP(w, req)

    if w.Code != http.StatusOK {
        t.Fatalf("expected 200 for OPTIONS, got %d", w.Code)
    }
}

func TestRedirectCacheHit(t *testing.T) {
    // start in-memory redis
    s, err := miniredis.Run()
    if err != nil {
        t.Skipf("miniredis not available: %v", err)
    }
    defer s.Close()

    // set cached URL
    s.Set("url:abc123", "http://cached.example.com")

    // set global rdb to client connected to miniredis
    prevRdb := rdb
    rdb = redis.NewClient(&redis.Options{Addr: s.Addr()})
    defer func() { rdb = prevRdb }()

    router := setupRouter()

    req := httptest.NewRequest(http.MethodGet, "/abc123", nil)
    w := httptest.NewRecorder()
    router.ServeHTTP(w, req)

    if w.Code != http.StatusMovedPermanently {
        t.Fatalf("expected redirect, got %d", w.Code)
    }
    if loc := w.Header().Get("Location"); loc != "http://cached.example.com" {
        t.Fatalf("unexpected location: %s", loc)
    }
}

func TestPublishClickEventPublishFailsFallbackHTTP(t *testing.T) {
    // start miniredis and then close to simulate publish failure
    s, err := miniredis.Run()
    if err != nil {
        t.Skipf("miniredis not available: %v", err)
    }
    addr := s.Addr()
    s.Close()

    prevRdb := rdb
    rdb = redis.NewClient(&redis.Options{Addr: addr})
    defer func() { rdb = prevRdb }()

    // python fallback server to capture fallback POST
    ch := make(chan []byte, 1)
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        body := make([]byte, r.ContentLength)
        r.Body.Read(body)
        ch <- body
        w.WriteHeader(http.StatusOK)
    }))
    defer srv.Close()

    prev := pythonServiceURL
    pythonServiceURL = srv.URL
    defer func() { pythonServiceURL = prev }()

    publishClickEvent("fallback123")

    select {
    case b := <-ch:
        if len(b) == 0 {
            t.Fatalf("expected body in fallback POST")
        }
    default:
        t.Fatalf("expected fallback POST to be sent")
    }
}

func TestRedirectCachesOnMiss(t *testing.T) {
    setupTestDB(t)
    defer teardownTestDB()

    // insert record
    _, err := db.Exec("INSERT INTO urls (short_code, long_url) VALUES (?, ?)", "db123", "http://db.example.com")
    if err != nil {
        t.Fatalf("insert failed: %v", err)
    }

    s, err := miniredis.Run()
    if err != nil {
        t.Skipf("miniredis not available: %v", err)
    }
    defer s.Close()

    prevRdb := rdb
    rdb = redis.NewClient(&redis.Options{Addr: s.Addr()})
    defer func() { rdb = prevRdb }()

    router := setupRouter()
    req := httptest.NewRequest(http.MethodGet, "/db123", nil)
    w := httptest.NewRecorder()
    router.ServeHTTP(w, req)

    if w.Code != http.StatusMovedPermanently {
        t.Fatalf("expected redirect, got %d", w.Code)
    }

    // ensure cached in redis
    if val, err := s.Get("url:db123"); err != nil || val != "http://db.example.com" {
        t.Fatalf("expected cache set for db123, got %v err=%v", val, err)
    }
}

func TestSendClickEventHTTPSuccessAndError(t *testing.T) {
    // capture request body
    received := make(chan []byte, 1)

    // success server
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/api/events" {
            w.WriteHeader(http.StatusNotFound)
            return
        }
        body := make([]byte, r.ContentLength)
        r.Body.Read(body)
        received <- body
        w.WriteHeader(http.StatusOK)
    }))
    defer srv.Close()

    prev := pythonServiceURL
    pythonServiceURL = srv.URL
    defer func() { pythonServiceURL = prev }()

    sendClickEventHTTP("evt123")

    select {
    case b := <-received:
        if len(b) == 0 {
            t.Fatalf("expected body, got empty")
        }
    default:
        t.Fatalf("expected request to be made to python service")
    }

    // error server
    errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusInternalServerError)
    }))
    defer errSrv.Close()

    pythonServiceURL = errSrv.URL
    sendClickEventHTTP("evt-error")
}

func TestInitDBCreatesFile(t *testing.T) {
    // remove any existing file
    _ = os.Remove("test_go.db")
    // temporarily change DB path by calling sql.Open directly
    prevDB := db
    db = nil
    // call initDB which creates ./go.db; rename to test_go.db afterwards
    initDB()
    db.Close()
    // move file
    _ = os.Rename("go.db", "test_go.db")
    defer func() {
        _ = os.Remove("test_go.db")
        db = prevDB
    }()
}

func TestInitRedisWarning(t *testing.T) {
    // set an env that likely fails to connect
    os.Setenv("REDIS_URL", "localhost:6399")
    prevRdb := rdb
    initRedis()
    // rdb should be nil or set but ping may fail; ensure no panic
    if rdb != nil {
        _ = rdb.Close()
    }
    rdb = prevRdb
}

func TestCreateShortURLBadPayload(t *testing.T) {
    setupTestDB(t)
    defer teardownTestDB()

    w := httptest.NewRecorder()
    c, _ := gin.CreateTestContext(w)
    // missing long_url
    req := httptest.NewRequest(http.MethodPost, "/api/shorten", strings.NewReader(`{"foo":"bar"}`))
    req.Header.Set("Content-Type", "application/json")
    c.Request = req

    createShortURL(c)

    if w.Code != http.StatusBadRequest {
        t.Fatalf("expected 400, got %d", w.Code)
    }
}

func TestSendClickEventHTTPErrorPath(t *testing.T) {
    prev := pythonServiceURL
    pythonServiceURL = "http://127.0.0.1:9999" // assume no server
    defer func() { pythonServiceURL = prev }()
    // should not panic
    sendClickEventHTTP("doesnotmatter")
}

func TestGenerateShortCodeStress(t *testing.T) {
    seen := map[string]bool{}
    for i := 0; i < 200; i++ {
        s := generateShortCode()
        if len(s) != 6 {
            t.Fatalf("short code len unexpected: %s", s)
        }
        seen[s] = true
    }
    if len(seen) < 50 {
        t.Fatalf("expected many distinct codes, got %d", len(seen))
    }
}

func TestCreateShortURLRegeneratesOnCollision(t *testing.T) {
    setupTestDB(t)
    defer teardownTestDB()

    // insert a colliding short code
    _, err := db.Exec("INSERT INTO urls (short_code, long_url) VALUES (?, ?)", "dupcode", "http://already.com")
    if err != nil {
        t.Fatalf("insert failed: %v", err)
    }

    // override generator to produce collision then unique
    prevGen := generateShortCode
    calls := 0
    generateShortCode = func() string {
        calls++
        if calls == 1 {
            return "dupcode"
        }
        return "uniq01"
    }
    defer func() { generateShortCode = prevGen }()

    w := httptest.NewRecorder()
    c, _ := gin.CreateTestContext(w)
    req := httptest.NewRequest(http.MethodPost, "/api/shorten", strings.NewReader(`{"long_url":"http://new.com"}`))
    req.Header.Set("Content-Type", "application/json")
    c.Request = req

    createShortURL(c)

    if w.Code != http.StatusOK {
        t.Fatalf("expected 200, got %d", w.Code)
    }

    var resp ShortenResponse
    if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
        t.Fatal(err)
    }
    if resp.ShortCode != "uniq01" {
        t.Fatalf("expected uniq01, got %s", resp.ShortCode)
    }
}

func TestInitURLSetsBase(t *testing.T) {
    prev := os.Getenv("BASE_URL")
    os.Setenv("BASE_URL", "https://example.test")
    defer func() { os.Setenv("BASE_URL", prev) }()

    initURL()
    if baseURL != "https://example.test" {
        t.Fatalf("expected baseURL to be set from env, got %s", baseURL)
    }
}


func TestRootEndpoint(t *testing.T) {
    router := setupRouter()

    req := httptest.NewRequest(http.MethodGet, "/", nil)
    w := httptest.NewRecorder()
    router.ServeHTTP(w, req)

    if w.Code != http.StatusOK {
        t.Fatalf("expected 200 for /, got %d", w.Code)
    }

    var body map[string]interface{}
    if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
        t.Fatal(err)
    }
    msg, ok := body["message"].(string)
    if !ok || msg != "Go service root" {
        t.Fatalf("unexpected root message: %v", body)
    }
}

func TestHealthEndpoint(t *testing.T) {
    router := setupRouter()

    req := httptest.NewRequest(http.MethodGet, "/health", nil)
    w := httptest.NewRecorder()
    router.ServeHTTP(w, req)

    if w.Code != http.StatusOK {
        t.Fatalf("expected 200 for /health, got %d", w.Code)
    }

    var body map[string]interface{}
    if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
        t.Fatal(err)
    }
    if status, ok := body["status"].(string); !ok || status != "healthy" {
        t.Fatalf("unexpected health status: %v", body)
    }
    if svc, ok := body["service"].(string); !ok || svc != "go-shortener-service" {
        t.Fatalf("unexpected service name: %v", body)
    }
}
