# GoMigrate - Standalone Database Migration Tool
# @file Makefile
# @description Build and distribution configuration for the standalone migration tool
# @author Admilson B. F. Cossa
# @date 2025-01-28
# @path pkg/core/database/migrations/Makefile

# Variables
BINARY_NAME=gomigrate
VERSION?=1.0.0
BUILD_DIR=build
DIST_DIR=dist
CMD_DIR=cmd
PKG_DIR=.

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Build flags
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -s -w"
BUILD_FLAGS=-trimpath

# Platforms for cross-compilation
PLATFORMS=windows/amd64 linux/amd64 darwin/amd64 linux/arm64 darwin/arm64

.PHONY: all build clean test deps tidy fmt vet lint install uninstall cross-compile package help

# Default target
all: clean deps test build

# Build the binary
build:
	@echo "Building $(BINARY_NAME) v$(VERSION)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./$(CMD_DIR)
	@echo "✓ Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# Build for Windows
build-windows:
	@echo "Building $(BINARY_NAME) for Windows..."
	@mkdir -p $(BUILD_DIR)
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME).exe ./$(CMD_DIR)
	@echo "✓ Windows build complete: $(BUILD_DIR)/$(BINARY_NAME).exe"

# Build for Linux
build-linux:
	@echo "Building $(BINARY_NAME) for Linux..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux ./$(CMD_DIR)
	@echo "✓ Linux build complete: $(BUILD_DIR)/$(BINARY_NAME)-linux"

# Build for macOS
build-darwin:
	@echo "Building $(BINARY_NAME) for macOS..."
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin ./$(CMD_DIR)
	@echo "✓ macOS build complete: $(BUILD_DIR)/$(BINARY_NAME)-darwin"

# Cross-compile for all platforms
cross-compile:
	@echo "Cross-compiling for all platforms..."
	@mkdir -p $(DIST_DIR)
	@for platform in $(PLATFORMS); do \
		GOOS=$$(echo $$platform | cut -d'/' -f1); \
		GOARCH=$$(echo $$platform | cut -d'/' -f2); \
		output_name=$(BINARY_NAME)-$$GOOS-$$GOARCH; \
		if [ $$GOOS = "windows" ]; then output_name=$$output_name.exe; fi; \
		echo "Building for $$GOOS/$$GOARCH..."; \
		GOOS=$$GOOS GOARCH=$$GOARCH $(GOBUILD) $(BUILD_FLAGS) $(LDFLAGS) -o $(DIST_DIR)/$$output_name ./$(CMD_DIR); \
	done
	@echo "✓ Cross-compilation complete"

# Package binaries
package: cross-compile
	@echo "Creating distribution packages..."
	@cd $(DIST_DIR) && \
	for file in $(BINARY_NAME)-*; do \
		if [[ $$file == *".exe" ]]; then \
			zip $${file%.*}.zip $$file; \
		else \
			tar -czf $$file.tar.gz $$file; \
		fi; \
	done
	@echo "✓ Packages created in $(DIST_DIR)/"

# Run tests
test: test-unit
	@echo "✓ Tests complete"

# Run unit tests only
test-unit:
	@echo "Running unit tests..."
	$(GOTEST) -v -race -coverprofile=coverage.out ./... -short
	@echo "✓ Unit tests complete"

# Run integration tests with dockerized databases
test-integration: docker-up
	@echo "Waiting for databases to be ready..."
	@sleep 10
	@echo "Running integration tests..."
	$(GOTEST) -v ./tests/integration/... -timeout=10m -race
	@$(MAKE) docker-down
	@echo "✓ Integration tests complete"

# Run all tests (unit + integration)
test-all: test-unit test-integration
	@echo "✓ All tests complete"

# Run tests with coverage
test-coverage: test-unit
	@echo "Generating coverage report..."
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "✓ Coverage report generated: coverage.html"

# Docker targets for integration testing
docker-up:
	@echo "Starting test databases..."
	docker-compose -f docker-compose.test.yml up -d
	@echo "Waiting for databases to initialize..."
	@sleep 15
	@$(MAKE) docker-health-check

docker-down:
	@echo "Stopping test databases..."
	docker-compose -f docker-compose.test.yml down -v

docker-logs:
	docker-compose -f docker-compose.test.yml logs

docker-health-check:
	@echo "Checking database health..."
	@echo "PostgreSQL:"
	@docker exec migrations-postgres-test pg_isready -U test_user -d migrations_test || echo "PostgreSQL not ready"
	@echo "MySQL:"
	@docker exec migrations-mysql-test mysqladmin ping -h localhost -u test_user -ptest_password || echo "MySQL not ready"
	@echo "SQLite container:"
	@docker exec migrations-sqlite-test ls -la /data || echo "SQLite container not ready"

docker-restart: docker-down docker-up

# Database-specific test targets
test-postgres:
	@echo "Starting PostgreSQL..."
	docker-compose -f docker-compose.test.yml up -d postgres-test
	@sleep 10
	@echo "Running PostgreSQL integration tests..."
	INTEGRATION_DB=postgres $(GOTEST) -v ./tests/integration/... -timeout=5m
	docker-compose -f docker-compose.test.yml stop postgres-test

test-mysql:
	@echo "Starting MySQL..."
	docker-compose -f docker-compose.test.yml up -d mysql-test
	@sleep 15
	@echo "Running MySQL integration tests..."
	INTEGRATION_DB=mysql $(GOTEST) -v ./tests/integration/... -timeout=5m
	docker-compose -f docker-compose.test.yml stop mysql-test

test-sqlite:
	@echo "Running SQLite integration tests..."
	INTEGRATION_DB=sqlite $(GOTEST) -v ./tests/integration/... -timeout=5m

# Run benchmarks
benchmark:
	@echo "Running benchmarks..."
	$(GOTEST) -bench=. -benchmem ./...

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GOGET) -d ./...
	@echo "✓ Dependencies downloaded"

# Tidy dependencies
tidy:
	@echo "Tidying dependencies..."
	$(GOMOD) tidy
	@echo "✓ Dependencies tidied"

# Format code
fmt:
	@echo "Formatting code..."
	$(GOCMD) fmt ./...
	@echo "✓ Code formatted"

# Vet code
vet:
	@echo "Vetting code..."
	$(GOCMD) vet ./...
	@echo "✓ Code vetted"

# Lint code (requires golangci-lint)
lint:
	@echo "Linting code..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
		echo "✓ Code linted"; \
	else \
		echo "⚠ golangci-lint not found, skipping lint"; \
	fi

# Install the binary to GOPATH/bin
install: build
	@echo "Installing $(BINARY_NAME)..."
	@cp $(BUILD_DIR)/$(BINARY_NAME) $(GOPATH)/bin/
	@echo "✓ $(BINARY_NAME) installed to $(GOPATH)/bin/"

# Uninstall the binary from GOPATH/bin
uninstall:
	@echo "Uninstalling $(BINARY_NAME)..."
	@rm -f $(GOPATH)/bin/$(BINARY_NAME)
	@echo "✓ $(BINARY_NAME) uninstalled"

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR)
	@rm -rf $(DIST_DIR)
	@rm -f coverage.out coverage.html
	@rm -f tests/integration/testdata/*.db*
	@$(MAKE) docker-down 2>/dev/null || true
	$(GOCLEAN)
	@echo "✓ Clean complete"

# Development setup
dev-setup: deps
	@echo "Setting up development environment..."
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "Installing golangci-lint..."; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(GOPATH)/bin v1.54.2; \
	fi
	@echo "✓ Development environment ready"

# Run the tool locally
run: build
	@echo "Running $(BINARY_NAME)..."
	./$(BUILD_DIR)/$(BINARY_NAME) $(ARGS)

# Create a new release
release: clean test cross-compile package
	@echo "Release v$(VERSION) created successfully!"
	@echo "Artifacts available in $(DIST_DIR)/"

# Docker build
docker-build:
	@echo "Building Docker image..."
	docker build -t $(BINARY_NAME):$(VERSION) .
	docker tag $(BINARY_NAME):$(VERSION) $(BINARY_NAME):latest
	@echo "✓ Docker image built: $(BINARY_NAME):$(VERSION)"

# Docker run
docker-run:
	@echo "Running Docker container..."
	docker run --rm -it $(BINARY_NAME):latest $(ARGS)

# Generate documentation
docs:
	@echo "Generating documentation..."
	@mkdir -p docs
	./$(BUILD_DIR)/$(BINARY_NAME) --help > docs/help.txt
	@echo "✓ Documentation generated in docs/"

# Check for security vulnerabilities
security:
	@echo "Checking for security vulnerabilities..."
	@if command -v gosec >/dev/null 2>&1; then \
		gosec ./...; \
		echo "✓ Security check complete"; \
	else \
		echo "⚠ gosec not found, install with: go install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest"; \
	fi

# Performance profiling
profile: build
	@echo "Running performance profile..."
	./$(BUILD_DIR)/$(BINARY_NAME) --cpuprofile=cpu.prof --memprofile=mem.prof $(ARGS)
	@echo "✓ Profiles generated: cpu.prof, mem.prof"

# Show help
help:
	@echo "GoMigrate Build System"
	@echo "====================="
	@echo ""
	@echo "Available targets:"
	@echo "  build           Build the binary for current platform"
	@echo "  build-windows   Build for Windows"
	@echo "  build-linux     Build for Linux"
	@echo "  build-darwin    Build for macOS"
	@echo "  cross-compile   Build for all platforms"
	@echo "  package         Create distribution packages"
	@echo "  test            Run unit tests"
	@echo "  test-unit       Run unit tests only"
	@echo "  test-integration Run integration tests with Docker"
	@echo "  test-all        Run all tests (unit + integration)"
	@echo "  test-coverage   Run tests with coverage report"
	@echo "  test-postgres   Run integration tests against PostgreSQL only"
	@echo "  test-mysql      Run integration tests against MySQL only"
	@echo "  test-sqlite     Run integration tests against SQLite only"
	@echo "  docker-up       Start test database containers"
	@echo "  docker-down     Stop test database containers"
	@echo "  docker-logs     Show database container logs"
	@echo "  docker-restart  Restart database containers"
	@echo "  benchmark       Run benchmarks"
	@echo "  deps            Download dependencies"
	@echo "  tidy            Tidy dependencies"
	@echo "  fmt             Format code"
	@echo "  vet             Vet code"
	@echo "  lint            Lint code (requires golangci-lint)"
	@echo "  install         Install binary to GOPATH/bin"
	@echo "  uninstall       Remove binary from GOPATH/bin"
	@echo "  clean           Clean build artifacts"
	@echo "  dev-setup       Setup development environment"
	@echo "  run             Run the tool locally (use ARGS=... for arguments)"
	@echo "  release         Create a new release"
	@echo "  docker-build    Build Docker image"
	@echo "  docker-run      Run Docker container"
	@echo "  docs            Generate documentation"
	@echo "  security        Check for security vulnerabilities"
	@echo "  profile         Run performance profiling"
	@echo "  help            Show this help message"
	@echo ""
	@echo "Examples:"
	@echo "  make build"
	@echo "  make test"
	@echo "  make cross-compile"
	@echo "  make run ARGS='--help'"
	@echo "  make release VERSION=1.1.0"