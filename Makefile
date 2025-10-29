.PHONY: dev prod clean build build-frontend run deps test fmt lint check help kill-dev image

# Variables
DATA_DIR := ./data
DB_FILE := $(DATA_DIR)/discopanel.db
FRONTEND_DIR := web/discopanel
DISCOPANEL_BIN:= build/discopanel
#DISCOSUPPORT_URL := http://localhost:8911

# Development mode - runs backend and frontend concurrently
run:
	@echo "Starting development environment..."
	@mkdir -p $(DATA_DIR)
	@echo "Starting backend server with frontend dev server..."
	@trap 'echo "Stopping all processes..."; kill $$(jobs -p) 2>/dev/null; wait; exit' INT TERM; \
	cd $(FRONTEND_DIR) && npm run dev & \
	FRONTEND_PID=$$!; \
	go run cmd/discopanel/main.go & \
	BACKEND_PID=$$!; \
	wait $$BACKEND_PID $$FRONTEND_PID

dev: clean run

# Production build and run
prod: build-frontend
	@echo "Building for production..."
	@mkdir -p $(DATA_DIR)
	go build -tags embed -o $(DISCOPANEL_BIN) cmd/discopanel/main.go

# Build frontend for production
build-frontend:
	@echo "Building frontend..."
	cd $(FRONTEND_DIR) && npm run build

# Build backend with embedded frontend
build: build-frontend
	@echo "Building backend with embedded frontend..."
	go build -o $(DISCOPANEL_BIN) cmd/discopanel/main.go

# Build and push Docker image to :dev tag
image:
	@echo "Building and pushing Docker image..."
	@bash scripts/build.sh

# Clean development data
clean:
	@echo "Cleaning development data..."
	@if [ -d "$(DATA_DIR)" ]; then \
		echo "Removing data directory..."; \
		rm -rf $(DATA_DIR); \
	fi
	@if [ -f "$(DISCOPANEL_BIN)" ]; then \
		echo "Removing backend binary..."; \
		rm -f $(DISCOPANEL_BIN); \
	fi
	@if [ -f "discopanel.db" ]; then \
		echo "Removing old database file..."; \
		rm -f discopanel.db; \
	fi
	@echo "Clean complete!"

# Kill any orphaned dev processes
kill-dev:
	@echo "Killing orphaned development processes..."
	@pkill -f "npm run dev" || true
	@pkill -f "vite" || true
	@pkill -f "go run cmd/discopanel/main.go" || true
	@pkill -f "discopanel" || true
	@echo "Cleanup complete!"

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
	@echo "  make build        - Build standalone binary with embedded frontend"
	@echo "  make prod         - Build and run in production mode"
	@echo "  make image        - Build and push Docker image to :dev tag"
	@echo "  make clean        - Remove data directory and build artifacts"
	@echo "  make kill-dev     - Kill any orphaned dev processes"
	@echo "  make deps         - Install all dependencies"
	@echo "  make test         - Run tests"
	@echo "  make fmt          - Format code"
	@echo "  make lint         - Lint code"
	@echo "  make check        - Type check frontend"
	@echo "  make help         - Show this help message"