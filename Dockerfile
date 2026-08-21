# ==========================================
# Build Stage (Compile Go Binary)
# ==========================================
FROM golang:1.24-alpine AS builder

# Install SSL certificates and build tools
RUN apk add --no-cache ca-certificates git tzdata

WORKDIR /app

ENV GOTOOLCHAIN=auto

# Copy dependency definition files first (for Docker layer caching)
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire backend source code
COPY . .

# Build statically linked binary optimized for production
RUN CGO_ENABLED=0 GOOS=linux go build -a -ldflags="-w -s" -o /app/server ./cmd/server

# ==========================================
# Final Production Stage (Minimal Alpine)
# ==========================================
FROM alpine:latest

# Install CA Certificates for HTTPS requests (MongoDB Atlas, Resend, Cloudinary)
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy compiled binary from builder stage
COPY --from=builder /app/server /app/server

# Copy docs for Swagger UI static files
COPY --from=builder /app/docs /app/docs

# Expose default port
EXPOSE 8080

# Run server binary
ENTRYPOINT ["/app/server"]
