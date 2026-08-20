.PHONY: build run dev clean test lint migrate

# Application binary name
APP_NAME=smart-invest-backend
BUILD_DIR=bin

# Build the application
build:
	@echo "Building $(APP_NAME)..."
	@mkdir -p $(BUILD_DIR)
	@go build -o $(BUILD_DIR)/$(APP_NAME) ./cmd/server/
	@echo "Build complete: $(BUILD_DIR)/$(APP_NAME)"

# Run the application
run: build
	@echo "Starting $(APP_NAME)..."
	@./$(BUILD_DIR)/$(APP_NAME)

# Run with hot reload (requires 'air' - install with: go install github.com/air-verse/air@latest)
dev:
	@echo "Starting development server with hot reload..."
	@air -c .air.toml 2>/dev/null || ~/go/bin/air -c .air.toml 2>/dev/null || go run ./cmd/server/

# Run tests
test:
	@echo "Running tests..."
	@go test -v -cover ./...

# Run linter (requires golangci-lint)
lint:
	@echo "Running linter..."
	@golangci-lint run ./...

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@rm -rf tmp

# Verify the project compiles
vet:
	@echo "Running go vet..."
	@go vet ./...

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	@go mod tidy
	@go mod download

# Run all checks
check: vet lint test
	@echo "All checks passed!"
