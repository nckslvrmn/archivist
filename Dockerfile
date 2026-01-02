# Build stage
FROM golang:1.25-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git gcc musl-dev sqlite-dev

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -ldflags="-w -s" -o archivist ./cmd/archivist

# Runtime stage
FROM alpine:latest

# Install runtime dependencies
RUN apk --no-cache add ca-certificates sqlite tzdata

# Create non-root user
RUN addgroup -g 1000 archivist && \
    adduser -D -u 1000 -G archivist archivist

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/archivist .

# Copy web static files
COPY --from=builder /app/web/static ./web/static

# Create required directories
RUN mkdir -p /data/sources /data/config /data/temp && \
    chown -R archivist:archivist /app /data

# Switch to non-root user
USER archivist

# Expose HTTP port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/api/v1/system/health || exit 1

# Run the application
CMD ["./archivist"]
