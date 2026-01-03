# Build stage
FROM golang:1.25-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git gcc musl-dev sqlite-dev

WORKDIR /app

# Copy go mod files first for dependency caching
COPY go.mod go.sum ./

# Download dependencies (cached unless go.mod/go.sum changes)
RUN go mod download

# Copy only Go source code (cached unless .go files change)
COPY cmd/ ./cmd/
COPY internal/ ./internal/

# Build the binary
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -ldflags="-w -s" -o archivist ./cmd/archivist

# Runtime stage
FROM alpine:latest

# Install runtime dependencies
RUN apk --no-cache add ca-certificates sqlite tzdata

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/archivist .

# Copy web files directly (not from builder)
COPY web/ ./web/

# Create required directories
RUN mkdir -p /data/sources /data/config /data/temp

# Expose HTTP port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/api/v1/system/health || exit 1

# Run the application
CMD ["./archivist"]
