# Smart Invest Solutions — Backend API

A modern Go backend API for Smart Invest Solutions, built with clean architecture principles.

## 🛠️ Tech Stack

- **Language**: Go 1.22+
- **HTTP Framework**: [Gin](https://github.com/gin-gonic/gin)
- **Database**: [MongoDB Atlas](https://www.mongodb.com/atlas) via [Official MongoDB Go Driver v2](https://go.mongodb.org/)
- **Logging**: [Zerolog](https://github.com/rs/zerolog)
- **Password Hashing**: bcrypt via `golang.org/x/crypto`

## 📁 Project Structure

```
smart_invest_solutions_app_backend/
├── cmd/
│   └── server/
│       └── main.go              # Application entry point
├── internal/
│   ├── config/
│   │   └── config.go            # Environment configuration
│   ├── database/
│   │   └── mongodb.go           # MongoDB connection & client
│   ├── domain/
│   │   └── user.go              # Domain entities & interfaces
│   ├── repository/
│   │   └── user_repository.go   # MongoDB data access layer
│   ├── service/
│   │   └── user_service.go      # Business logic layer
│   ├── handler/
│   │   └── user_handler.go      # HTTP request handlers
│   ├── middleware/
│   │   └── cors.go              # CORS, logging & recovery middleware
│   └── router/
│       └── router.go            # Route definitions
├── pkg/
│   └── response/
│       └── response.go          # Standardized API responses
├── migrations/
│   └── migrations.go            # Database migration runner
├── .env.example                 # Environment variable template
├── .gitignore                   # Git ignore rules
├── Makefile                     # Build & dev commands
├── go.mod                       # Go module definition
└── go.sum                       # Go dependency checksums
```

## 🚀 Getting Started

### Prerequisites

- Go 1.22 or later
- MongoDB Atlas account (or local MongoDB instance)

### Setup

1. **Clone and navigate to the project:**
   ```bash
   cd smart_invest_solutions_app_backend
   ```

2. **Copy the environment file and configure:**
   ```bash
   cp .env.example .env
   ```
   Edit `.env` and set your MongoDB Atlas connection string.

3. **Install dependencies:**
   ```bash
   go mod tidy
   ```

4. **Run the server:**
   ```bash
   make run
   # or
   go run ./cmd/server/
   ```

5. **Development mode (with hot reload):**
   ```bash
   # Install air first: go install github.com/air-verse/air@latest
   make dev
   ```

### Verify

```bash
curl http://localhost:8080/health
```

## 📡 API Endpoints

### Health Check
| Method | Endpoint   | Description          |
|--------|-----------|----------------------|
| GET    | `/health` | Server health status |

### Users (v1)
| Method | Endpoint                    | Description            |
|--------|----------------------------|------------------------|
| POST   | `/api/v1/users/register`   | Register a new user    |
| GET    | `/api/v1/users`            | List all users (paginated) |
| GET    | `/api/v1/users/:id`        | Get user by ID         |
| PUT    | `/api/v1/users/:id`        | Update user            |
| DELETE | `/api/v1/users/:id`        | Delete user            |

## 🏗️ Architecture

This project follows **Clean Architecture** principles:

- **Domain Layer** (`internal/domain/`): Core entities, DTOs, and interface definitions
- **Repository Layer** (`internal/repository/`): MongoDB data access implementations
- **Service Layer** (`internal/service/`): Business logic orchestration
- **Handler Layer** (`internal/handler/`): HTTP request handling
- **Router Layer** (`internal/router/`): Route setup and dependency wiring

## 📋 Available Make Commands

```bash
make build    # Build the application binary
make run      # Build and run the application
make dev      # Run with hot reload (requires air)
make test     # Run all tests
make vet      # Run go vet
make lint     # Run golangci-lint
make clean    # Remove build artifacts
make deps     # Download dependencies
make check    # Run vet + lint + tests
```
