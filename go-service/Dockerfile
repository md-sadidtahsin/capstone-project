FROM golang:1.24-alpine

WORKDIR /app

# Install gcc and build tools for CGO (required by go-sqlite3)
RUN apk add --no-cache gcc musl-dev sqlite-dev

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY main.go ./

# Build the application with CGO enabled and proper tags for Alpine/musl
ENV CGO_ENABLED=1
RUN go build -tags "libsqlite3" -o urlshortener main.go

# Expose port
EXPOSE 8000

# Run the binary
CMD ["./urlshortener"]
