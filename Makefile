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
	@if command -v air > /dev/null 2>&1; then \
		air -c .air.toml; \
	elif [ -f "$(HOME)/go/bin/air" ]; then \
		$(HOME)/go/bin/air -c .air.toml; \
	else \
		echo "Air not found. Running with go run instead..."; \
		go run ./cmd/server/; \
	fi

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

# ==========================================
# Swagger Documentation
# ==========================================

# Generate Swagger docs from annotations
swagger-gen:
	@echo "Generating Swagger docs..."
	@if command -v swag > /dev/null 2>&1; then \
		swag init -g cmd/server/main.go -o docs --parseDependency --parseInternal; \
	elif [ -f "$(HOME)/go/bin/swag" ]; then \
		$(HOME)/go/bin/swag init -g cmd/server/main.go -o docs --parseDependency --parseInternal; \
	else \
		echo "swag not found. Install: go install github.com/swaggo/swag/cmd/swag@latest"; \
	fi

# ==========================================
# Prisma Schema Management
# ==========================================

# Validate Prisma schema
prisma-validate:
	@echo "Validating Prisma schema..."
	@npx prisma validate

# Push schema to MongoDB (create collections & indexes)
prisma-push:
	@echo "Pushing schema to MongoDB..."
	@npx prisma db push

# Open Prisma Studio (visual database browser)
prisma-studio:
	@echo "Opening Prisma Studio..."
	@npx prisma studio

# Format Prisma schema file
prisma-format:
	@echo "Formatting Prisma schema..."
	@npx prisma format

# Generate Prisma client
prisma-generate:
	@echo "Generating Prisma client..."
	@npx prisma generate
