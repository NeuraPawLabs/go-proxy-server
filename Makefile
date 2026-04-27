.PHONY: build build-linux build-windows build-windows-gui build-darwin build-resources clean clean-resources test run help frontend-build frontend-deps frontend-dev frontend-clean benchmark benchmark-linux benchmark-windows benchmark-darwin benchmark-all

# Binary name
BINARY_NAME=go-proxy-server
BENCHMARK_NAME=benchmark
MAIN_PATH=./cmd/server
BENCHMARK_PATH=./cmd/benchmark
OUTPUT_DIR=bin
RESOURCE_SCRIPT=./scripts/build_resources.sh
SYSO_FILE=$(MAIN_PATH)/resource_windows_amd64.syso

# Frontend
FRONTEND_DIR=web-ui
FRONTEND_DIST=$(FRONTEND_DIR)/dist
FRONTEND_SYNC_SCRIPT=./scripts/sync_frontend_dist.sh
GO_BUILD_TAGS=frontend_embed

# Build flags
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
VERSION_PKG=main.version
LDFLAGS=-s -w -X $(VERSION_PKG)=$(VERSION)
WINDOWS_GUI_LDFLAGS=-s -w -H=windowsgui -X $(VERSION_PKG)=$(VERSION)

# Default target
all: build

# Check if npm is installed
check-npm:
	@which npm > /dev/null || (echo "Error: npm is not installed. Please install Node.js and npm first." && exit 1)

# Install frontend dependencies
frontend-deps: check-npm
	@echo "Installing frontend dependencies..."
	@cd $(FRONTEND_DIR) && npm install

# Build frontend for production
frontend-build: check-npm
	@echo "Checking frontend dependencies..."
	@if [ ! -d "$(FRONTEND_DIR)/node_modules" ]; then \
		echo "node_modules not found, installing dependencies..."; \
		cd $(FRONTEND_DIR) && npm install; \
	fi
	@echo "Building frontend..."
	@cd $(FRONTEND_DIR) && npm run build
	@$(FRONTEND_SYNC_SCRIPT)
	@echo "Frontend build complete: $(FRONTEND_DIST)"

# Clean frontend build
frontend-clean:
	@echo "Cleaning frontend build..."
	@rm -rf $(FRONTEND_DIST)
	@rm -rf internal/web/dist
	@rm -rf $(FRONTEND_DIR)/node_modules

# Development: run frontend dev server
frontend-dev: check-npm
	@echo "Starting frontend dev server..."
	@cd $(FRONTEND_DIR) && npm run dev

# Build for current platform
build: frontend-build
	@echo "Building for current platform..."
	@mkdir -p $(OUTPUT_DIR)
	go build -tags "$(GO_BUILD_TAGS)" -ldflags "$(LDFLAGS)" -o $(OUTPUT_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "Build complete: $(OUTPUT_DIR)/$(BINARY_NAME)"

# Build for Linux
build-linux: frontend-build
	@echo "Building for Linux..."
	@mkdir -p $(OUTPUT_DIR)
	GOOS=linux GOARCH=amd64 go build -tags "$(GO_BUILD_TAGS)" -ldflags "$(LDFLAGS)" -o $(OUTPUT_DIR)/$(BINARY_NAME)-linux-amd64 $(MAIN_PATH)
	@echo "Build complete: $(OUTPUT_DIR)/$(BINARY_NAME)-linux-amd64"

# Build Windows resources (.syso file)
build-resources:
	@echo "Building Windows resources..."
	@bash $(RESOURCE_SCRIPT) || (echo ""; echo "ERROR: Failed to build Windows resources."; echo "See scripts/README.md for installation instructions."; echo ""; exit 1)

# Build for Windows (GUI / tray mode)
build-windows: frontend-build build-resources
	@echo "Building for Windows (GUI / tray mode)..."
	@mkdir -p $(OUTPUT_DIR)
	GOOS=windows GOARCH=amd64 go build -tags "$(GO_BUILD_TAGS)" -ldflags "$(WINDOWS_GUI_LDFLAGS)" -o $(OUTPUT_DIR)/$(BINARY_NAME).exe $(MAIN_PATH)
	@echo "Build complete: $(OUTPUT_DIR)/$(BINARY_NAME).exe"

# Legacy alias for Windows GUI build
build-windows-gui: build-windows
	@echo "Alias complete: $(OUTPUT_DIR)/$(BINARY_NAME).exe"

# Build for macOS
build-darwin: frontend-build
	@echo "Building for macOS..."
	@mkdir -p $(OUTPUT_DIR)
	GOOS=darwin GOARCH=amd64 go build -tags "$(GO_BUILD_TAGS)" -ldflags "$(LDFLAGS)" -o $(OUTPUT_DIR)/$(BINARY_NAME)-darwin-amd64 $(MAIN_PATH)
	@echo "Build complete: $(OUTPUT_DIR)/$(BINARY_NAME)-darwin-amd64"

# Build for all platforms
build-all: build-linux build-windows build-darwin
	@echo "All builds complete!"

# Build benchmark tool for current platform
benchmark:
	@echo "Building benchmark tool for current platform..."
	@mkdir -p $(OUTPUT_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(OUTPUT_DIR)/$(BENCHMARK_NAME) $(BENCHMARK_PATH)
	@echo "Build complete: $(OUTPUT_DIR)/$(BENCHMARK_NAME)"

# Build benchmark tool for Linux
benchmark-linux:
	@echo "Building benchmark tool for Linux..."
	@mkdir -p $(OUTPUT_DIR)
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(OUTPUT_DIR)/$(BENCHMARK_NAME)-linux-amd64 $(BENCHMARK_PATH)
	@echo "Build complete: $(OUTPUT_DIR)/$(BENCHMARK_NAME)-linux-amd64"

# Build benchmark tool for Windows
benchmark-windows:
	@echo "Building benchmark tool for Windows..."
	@mkdir -p $(OUTPUT_DIR)
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(OUTPUT_DIR)/$(BENCHMARK_NAME).exe $(BENCHMARK_PATH)
	@echo "Build complete: $(OUTPUT_DIR)/$(BENCHMARK_NAME).exe"

# Build benchmark tool for macOS
benchmark-darwin:
	@echo "Building benchmark tool for macOS..."
	@mkdir -p $(OUTPUT_DIR)
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(OUTPUT_DIR)/$(BENCHMARK_NAME)-darwin-amd64 $(BENCHMARK_PATH)
	@echo "Build complete: $(OUTPUT_DIR)/$(BENCHMARK_NAME)-darwin-amd64"

# Build benchmark tool for all platforms
benchmark-all: benchmark-linux benchmark-windows benchmark-darwin
	@echo "All benchmark builds complete!"

# Clean Windows resources
clean-resources:
	@echo "Cleaning Windows resources..."
	@rm -f $(SYSO_FILE)
	@rm -f $(MAIN_PATH)/rsrc_windows_amd64.syso
	@rm -rf winres/
	@echo "Resources cleaned!"

# Clean build artifacts
clean: clean-resources frontend-clean
	@echo "Cleaning build artifacts..."
	rm -rf $(OUTPUT_DIR)
	@echo "Clean complete!"

# Run tests
test:
	@echo "Running tests..."
	@if [ -d "internal/web/dist" ]; then \
		go test -v -tags "$(GO_BUILD_TAGS)" ./...; \
	else \
		go test -v ./...; \
	fi

# Run the application
run: frontend-build
	@echo "Running application..."
	go run -tags "$(GO_BUILD_TAGS)" $(MAIN_PATH)

# Install dependencies
deps:
	@echo "Installing dependencies..."
	go mod download
	go mod tidy

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Lint code
lint:
	@echo "Linting code..."
	golangci-lint run ./...

# Show help
help:
	@echo "Available targets:"
	@echo "  make build              - Build for current platform (includes frontend)"
	@echo "  make build-linux        - Build for Linux (includes frontend)"
	@echo "  make build-windows      - Build for Windows GUI/tray mode (includes frontend)"
	@echo "  make build-windows-gui  - Alias for build-windows"
	@echo "  make build-darwin       - Build for macOS (includes frontend)"
	@echo "  make build-resources    - Build Windows resource file (.syso)"
	@echo "  make build-all          - Build for all platforms (includes frontend)"
	@echo "  make benchmark          - Build benchmark tool for current platform"
	@echo "  make benchmark-linux    - Build benchmark tool for Linux"
	@echo "  make benchmark-windows  - Build benchmark tool for Windows"
	@echo "  make benchmark-darwin   - Build benchmark tool for macOS"
	@echo "  make benchmark-all      - Build benchmark tool for all platforms"
	@echo "  make frontend-build     - Build frontend only"
	@echo "  make frontend-dev       - Start frontend dev server (port 3000)"
	@echo "  make frontend-deps      - Install frontend dependencies"
	@echo "  make frontend-clean     - Clean frontend build and dependencies"
	@echo "  make clean              - Remove all build artifacts"
	@echo "  make clean-resources    - Remove Windows resource files"
	@echo "  make test               - Run tests"
	@echo "  make run                - Run the application"
	@echo "  make deps               - Install dependencies"
	@echo "  make fmt                - Format code"
	@echo "  make lint               - Lint code"
	@echo "  make help               - Show this help message"
