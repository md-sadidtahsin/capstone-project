# URL Shortener Microservices OSTAD Submission

A demo microservice architecture for a URL shortener application with four independently deployable services:

- **Go**: URL shortening and redirect service
- **Python**: dashboard, analytics, event processing, and orchestration
- **Node.js**: URL metadata enrichment
- **Redis**: caching and Pub/Sub event transport

This repository is intended as a capstone-style project that demonstrates service separation, event-driven communication, and polyglot data flows.

---

## Architecture Overview

The system is composed of multiple services with clear responsibilities:

- **Go service** (`go-service`): creates short codes, stores URL mappings in SQLite, performs redirects, caches URL lookups in Redis, and publishes click events.
- **Python service** (`python-service`): serves the web dashboard, coordinates URL creation, receives fallback click events, subscribes to Redis Pub/Sub events, and stores analytics in SQLite.
- **Node.js service** (`node-service`): retrieves page metadata for the long URL and stores it in SQLite.
- **Redis** (`redis`): provides caching for redirects and event delivery for click tracking.

### Data flow

1. User creates a short URL through the Python dashboard.
2. Python calls the Go service to generate a short code.
3. Python calls the Node.js service to fetch page metadata.
4. The Go service stores URLs in SQLite and uses Redis to cache lookups.
5. When a user clicks a short URL, Go redirects and publishes a click event.
6. Python consumes click events from Redis and saves analytics.

---

## Features

- Short URL creation and redirect
- Fast redirect caching with Redis
- Event-driven click tracking via Redis Pub/Sub
- HTTP fallback event sink when Redis is unavailable
- Metadata enrichment (title, description, favicon)
- Analytics dashboard for created URLs and click statistics
- Multiple language stack with Go, Python, and Node.js
- Containerized development using Docker Compose

---

## Prerequisites

- Docker
- Docker Compose
- Go 1.24+
- Python 3.8+ (3.14 recommended)
- Node.js 18+ / npm
- Redis 7+ (local development only, Docker Compose includes Redis)

> Docker Compose is the recommended way to run the full stack.

---

## Quick Start with Docker Compose

From the repository root:

```bash
docker-compose up --build
```

Then access the dashboard:

- Dashboard: `http://localhost:5000`
- Go service: `http://localhost:8000`
- Node.js service: `http://localhost:3000`

Stop the stack:

```bash
docker-compose down
```

Remove volumes (clears SQLite data):

```bash
docker-compose down -v
```

---

## Local Development

### 1. Start Redis

If you are not using Docker Compose, start a Redis instance locally on port `6379`.

### 2. Run the Go service

```bash
cd go-service
go mod download
BASE_URL=http://localhost:8000 REDIS_URL=localhost:6379 go run main.go
```

### 3. Run the Node.js service

```bash
cd node-service
npm install
npm start
```

### 4. Run the Python service

```bash
cd python-service
python -m venv venv
venv\Scripts\activate        # Windows
# source venv/bin/activate   # macOS/Linux
pip install -r requirements.txt
set GO_SERVICE_URL=http://localhost:8000
set NODE_SERVICE_URL=http://localhost:3000
set REDIS_URL=localhost:6379
python app.py
```

The Python dashboard will be available at `http://localhost:5000`.

---
## Workflows

### Local development workflow

1. Start a local Redis instance on `localhost:6379`.
2. Start the Go service on `http://localhost:8000`.
3. Start the Node.js service on `http://localhost:3000`.
4. Start the Python service on `http://localhost:5000`.
5. Use the Python dashboard to create short URLs and verify redirects with the Go service.
6. Inspect Redis activity, SQLite files, and console logs for event flow and metadata enrichment.

This workflow is ideal for debugging individual services, iterating quickly, and verifying how Redis Pub/Sub and fallback event delivery behave.

### Docker Compose workflow

1. Run `docker-compose up --build` from the repository root.
2. Wait for all containers to start: `redis`, `go-service`, `node-service`, and `python-service`.
3. Open `http://localhost:5000` to use the dashboard.
4. Use `docker-compose logs -f` to review logs and verify service startup and event processing.
5. When finished, run `docker-compose down`.

This workflow is the fastest way to bring the full stack online with consistent networking and environment variables.

### Kubernetes workflow

The `k8s/` folder includes manifests for deploying the microservices to a Kubernetes cluster.

Recommended flow:

1. Create the namespace: `kubectl apply -f k8s/namespace.yaml`
2. Deploy Redis: `kubectl apply -f k8s/redis.yaml`
3. Deploy the services: `kubectl apply -f k8s/go-deployment.yaml -f k8s/node-deployment.yaml -f k8s/python-deployment.yaml`
4. Expose the services with load balancers or an ingress resource: `kubectl apply -f k8s/ingress.yaml`
5. Verify pods and services: `kubectl get pods,svc -n urlshortner`

This workflow is best for testing the project in a cloud-like environment with Kubernetes service discovery and deployment manifests.

---
## Service Endpoints

### `go-service`

- `POST /api/shorten`
  - Request JSON: `{ "long_url": "https://example.com" }`
  - Response: short code and short URL
- `GET /api/go/:code`
  - Redirects to the original long URL

### `node-service`

- `POST /api/metadata`
  - Request JSON: `{ "short_code": "abc123", "long_url": "https://example.com" }`
  - Response: metadata record
- `GET /api/metadata/:short_code`
  - Returns stored metadata for one short code
- `GET /api/metadata`
  - Returns all stored metadata records
- `GET /health`
  - Health check endpoint

### `python-service`

- `GET /`
  - Dashboard UI
- `POST /create`
  - Create a new short URL from the web form
- `POST /api/events`
  - Receive click events from `go-service` as fallback when Redis is unavailable

---

## Testing

### Node.js

```bash
cd node-service
npm test
```

### Go

```bash
cd go-service
go test ./...
```

### Python

```bash
cd python-service
pytest
```

---

## Project Structure

- `docker-compose.yml` — full-stack service composition
- `go-service/` — Go redirect and shortening service
- `python-service/` — Flask dashboard and analytics service
- `node-service/` — metadata enrichment service
- `k8s/` — Kubernetes manifests for deployment
- `load-test/` — load testing script

---

## Notes

- The Go service caches URL lookups in Redis to improve redirect performance.
- The Python service subscribes to Redis `click_events` and stores analytics data.
- The Node.js service enriches new URLs with page titles and favicon information.
- Each service has its own local SQLite persistence file.

---

## License

This project is provided under the `MIT` license.
