.PHONY: dev prod clean build-frontend

# Variables
DATA_DIR := ./data
DB_FILE := $(DATA_DIR)/discopanel.db
FRONTEND_DIR := web/discopanel
BACKEND_BIN := discopanel

# Development mode - runs backend and frontend concurrently
dev: clean
	@echo "Starting development environment..."
	@mkdir -p $(DATA_DIR)
	@echo "Starting backend server with frontend dev server..."
	@trap 'kill %1' INT; \
	cd $(FRONTEND_DIR) && npm run dev & \
	go run cmd/discopanel/main.go

# Production build and run
prod: build-frontend
	@echo "Building for production..."
	@mkdir -p $(DATA_DIR)
	go build -o $(BACKEND_BIN) cmd/discopanel/main.go
	@echo "Starting production server..."
	./$(BACKEND_BIN)

# Build frontend for production
build-frontend:
	@echo "Building frontend..."
	cd $(FRONTEND_DIR) && npm run build

# Clean development data
clean:
	@echo "Cleaning development data..."
	@if [ -d "$(DATA_DIR)" ]; then \
		echo "Removing data directory..."; \
		rm -rf $(DATA_DIR); \
	fi
	@if [ -f "$(BACKEND_BIN)" ]; then \
		echo "Removing backend binary..."; \
		rm -f $(BACKEND_BIN); \
	fi
	@if [ -f "discopanel.db" ]; then \
		echo "Removing old database file..."; \
		rm -f discopanel.db; \
	fi
	@echo "Clean complete!"

# Install dependencies
deps:
	@echo "Installing Go dependencies..."
	go mod download
	@echo "Installing frontend dependencies..."
	cd $(FRONTEND_DIR) && npm install

# Run tests
test:
	@echo "Running Go tests..."
	go test ./...

# Format code
fmt:
	@echo "Formatting Go code..."
	go fmt ./...
	@echo "Formatting frontend code..."
	cd $(FRONTEND_DIR) && npm run format

# Lint code
lint:
	@echo "Linting frontend code..."
	cd $(FRONTEND_DIR) && npm run lint

# Type check frontend
check:
	@echo "Type checking frontend..."
	cd $(FRONTEND_DIR) && npm run check

# Help
help:
	@echo "Available commands:"
	@echo "  make dev          - Run in development mode (frontend + backend)"
	@echo "  make prod         - Build and run in production mode"
	@echo "  make clean        - Remove data directory and build artifacts"
	@echo "  make deps         - Install all dependencies"
	@echo "  make test         - Run tests"
	@echo "  make fmt          - Format code"
	@echo "  make lint         - Lint code"
	@echo "  make check        - Type check frontend"
	@echo "  make help         - Show this help message"