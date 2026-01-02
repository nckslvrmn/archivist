.PHONY: clean lint test run all

all: clean test lint build docker run

# Default target
help:
	@echo "Archivist - Makefile Commands"
	@echo ""
	@echo "  make test          - Run all tests"
	@echo "  make lint          - Run linters"
	@echo "  make clean         - Clean build artifacts"
	@echo "  make build         - Build the Go binary"
	@echo "  make run           - Run the application locally"
	@echo "  make docker        - Build the Docker image"
	@echo ""

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f archivist
	@echo "Clean complete"

# Run all tests
test:
	@echo "Running tests..."
	go test -v ./...

# Run linters
lint:
	@echo "Running linters..."
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not installed. Install from https://golangci-lint.run/"; exit 1; }
	golangci-lint run ./...

# Build the binary
build:
	@echo "Building archivist..."
	CGO_ENABLED=1 go build -o archivist ./cmd/archivist
	@echo "Build complete: ./archivist"

# Run the application locally
run: build
	@echo "Running archivist..."
	./archivist --root="$(CURDIR)/data"

# Build Docker image
docker:
	@echo "Building Docker image..."
	docker build -t archivist:latest .
	@echo "Docker image built: archivist:latest"
