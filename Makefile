.PHONY: clean lint vulncheck test run all

all: clean test lint build docker run

# Default target
help:
	@echo "Archivist - Makefile Commands"
	@echo ""
	@echo "  make test          - Run all tests"
	@echo "  make lint          - Run linters and formatting checks"
	@echo "  make vulncheck     - Report known vulnerabilities in dependencies"
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

# Run linters (same checks as CI)
lint:
	@echo "Checking formatting..."
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then echo "These files need gofmt:"; echo "$$unformatted"; exit 1; fi
	@echo "Running linters..."
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not installed. Install from https://golangci-lint.run/"; exit 1; }
	golangci-lint run ./...

# Report known vulnerabilities in dependencies
vulncheck:
	@echo "Running govulncheck..."
	@command -v govulncheck >/dev/null 2>&1 || go install golang.org/x/vuln/cmd/govulncheck@latest
	govulncheck ./...

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
