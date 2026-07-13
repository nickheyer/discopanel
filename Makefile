.PHONY: dev prod clean build build-frontend run deps test fmt lint check help kill-dev image images dev-docker dev-auth modules runtime agent proto proto-clean proto-lint proto-format proto-breaking gen dev-docs

DATA_DIR := ./data
DOCKER_DATA_DIR := /tmp/discopanel
DB_FILE := $(DATA_DIR)/discopanel.db
FRONTEND_DIR := web/discopanel
DISCOPANEL_BIN := build/discopanel
BUF_IMAGE := bufbuild/buf:latest
BUF_RUN := docker run --rm \
	--volume "$(shell pwd):/workspace" \
	--workdir /workspace \
	--user "$(shell id -u):$(shell id -g)" \
	--env HOME=/tmp \
	$(BUF_IMAGE)

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

restore:
	@echo "Restoring seeded db for dev"
	@mkdir -p $(DATA_DIR)
	cp dev/discopanel.db data/discopanel.db || echo "No saved dev state, starting new db"

dev: clean restore run

# Build and run with OIDC provider (Keycloak)
dev-auth-%: clean
	docker compose -f oidc/$*/docker-compose.yaml down -v --remove-orphans
	@docker run --rm -v /tmp:/tmp alpine rm -rf /tmp/discopanel
	@echo "Building and running with OIDC provider..."
	docker compose -f oidc/$*/docker-compose.yaml build --no-cache
	docker compose -f oidc/$*/docker-compose.yaml up

dev-docker: clean
	docker compose down -v --remove-orphans
	@docker run --rm -v /tmp:/tmp alpine rm -rf /tmp/discopanel
	@echo "Building and running with base compose..."
	docker compose build --no-cache
	docker compose up

dev-docs:
	cd docs/discopanel && npm run dev
	
# Production build and run
prod: build-frontend
	@echo "Building for production..."
	@mkdir -p $(DATA_DIR)
	go build -o $(DISCOPANEL_BIN) cmd/discopanel/main.go

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

# Published Java majors, docker.SupportedJavaVersions is the source
RUNTIME_JAVA_VERSIONS := $(shell go run ./cmd/javamajors 2>/dev/null)

# Published Graal majors, docker.GraalJavaVersions is the source
RUNTIME_GRAAL_VERSIONS := $(shell go run ./cmd/javamajors -graal 2>/dev/null)

# Git identity stamped into runtime images
RUNTIME_VERSION := $(shell git describe --always --dirty 2>/dev/null || echo dev)

# Pushes happen only in CI, local builds stay local
STAMP_DIR := build/.stamps

MODULE_NAMES := $(filter-out discopanel runtime,$(patsubst docker/Dockerfile.%,%,$(wildcard docker/Dockerfile.*)))

# Everything the runtime image bakes in, generated code excluded
RUNTIME_SRC := docker/Dockerfile.runtime go.mod go.sum \
	$(shell find cmd/runtime pkg/runtimespec proto -type f 2>/dev/null) \
	$(shell find agent -type f -not -path 'agent/build/*' -not -path 'agent/.gradle-home/*' -not -path 'agent/src/generated/*' 2>/dev/null)

# Module images copy the whole Go tree, track it coarsely
MODULE_SRC := go.mod go.sum \
	$(shell find cmd internal pkg proto -type f 2>/dev/null | grep -v '_test.go')

# Stamps for tags someone removed by hand die at parse time
ifneq ($(filter images runtime modules module-%,$(MAKECMDGOALS)),)
_STAMP_SYNC := $(shell mkdir -p $(STAMP_DIR); \
	for v in $(RUNTIME_JAVA_VERSIONS); do docker image inspect "nickheyer/discopanel-runtime:java$$v" >/dev/null 2>&1 || rm -f $(STAMP_DIR)/runtime-java$$v; done; \
	for v in $(RUNTIME_GRAAL_VERSIONS); do docker image inspect "nickheyer/discopanel-runtime:java$$v-graal" >/dev/null 2>&1 || rm -f $(STAMP_DIR)/runtime-graal-java$$v; done; \
	for m in $(MODULE_NAMES); do docker image inspect "nickheyer/discopanel-$$m:latest" >/dev/null 2>&1 || rm -f $(STAMP_DIR)/module-$$m; done)
endif

# Rebuilds a local tag and deletes the image it displaced
define build_image
	@old=$$(docker image inspect -f '{{.Id}}' "$(1)" 2>/dev/null || true); \
	docker build $(2) -t "$(1)" -f "$(3)" . || exit 1; \
	new=$$(docker image inspect -f '{{.Id}}' "$(1)"); \
	if [ -n "$$old" ] && [ "$$old" != "$$new" ]; then \
		echo "Removing replaced image $${old#sha256:}..."; \
		docker rmi "$$old" >/dev/null 2>&1 || true; \
	fi
endef

# Everything discopanel needs at runtime, built locally when stale
images: runtime modules
	@echo "All local images up to date!"

# Builds every runtime image variant locally when inputs changed
runtime: $(addprefix $(STAMP_DIR)/runtime-java,$(RUNTIME_JAVA_VERSIONS)) \
	$(addprefix $(STAMP_DIR)/runtime-graal-java,$(RUNTIME_GRAAL_VERSIONS))
	@echo "Runtime images up to date!"

$(STAMP_DIR)/runtime-java%: $(RUNTIME_SRC)
	@mkdir -p $(STAMP_DIR)
	@echo "Building nickheyer/discopanel-runtime:java$*..."
	$(call build_image,nickheyer/discopanel-runtime:java$*,--build-arg JAVA_VERSION=$* --build-arg RUNTIME_VERSION=$(RUNTIME_VERSION),docker/Dockerfile.runtime)
	@touch $@

$(STAMP_DIR)/runtime-graal-java%: $(RUNTIME_SRC)
	@mkdir -p $(STAMP_DIR)
	@echo "Building nickheyer/discopanel-runtime:java$*-graal..."
	$(call build_image,nickheyer/discopanel-runtime:java$*-graal,--build-arg JAVA_VERSION=$* --build-arg RUNTIME_FLAVOR=graal --build-arg RUNTIME_VERSION=$(RUNTIME_VERSION),docker/Dockerfile.runtime)
	@touch $@

# Builds all module images locally when inputs changed
modules: $(addprefix $(STAMP_DIR)/module-,$(MODULE_NAMES))
	@echo "Module images up to date!"

$(STAMP_DIR)/module-%: docker/Dockerfile.% $(MODULE_SRC)
	@mkdir -p $(STAMP_DIR)
	@echo "Building nickheyer/discopanel-$*:latest..."
	$(call build_image,nickheyer/discopanel-$*:latest,,docker/Dockerfile.$*)
	@touch $@

# Builds one module image locally (e.g., make module-status)
module-%: $(STAMP_DIR)/module-%
	@echo "Module $* image up to date!"

# Builds disco-agent jar via containerized Gradle
agent:
	docker run --rm \
		--volume "$(shell pwd)/agent:/agent" \
		--workdir /agent \
		--user "$(shell id -u):$(shell id -g)" \
		--env GRADLE_USER_HOME=/agent/.gradle-home \
		gradle:9-jdk21 gradle --no-daemon build

# Clean development data
clean:
	@echo "Cleaning development data..."
	@if [ -d "$(DATA_DIR)" ]; then \
		echo "Removing data directory..."; \
		rm -rf $(DATA_DIR); \
	fi
	@if [ -d "$(DOCKER_DATA_DIR)" ]; then \
		echo "Removing docker data directory..."; \
		docker run --rm -v $(DOCKER_DATA_DIR):/tmp alpine sh -c 'rm -rf /tmp/*'; \
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
	@echo "Updating buf dependencies (using Docker)..."
	$(BUF_RUN) dep update
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
lint: proto-lint
	@echo "Linting frontend code..."
	cd $(FRONTEND_DIR) && npm run lint

# Type check frontend
check:
	@echo "Type checking frontend..."
	cd $(FRONTEND_DIR) && npm run check

proto:
	@echo "Generating protocol buffer code (using Docker)..."
	$(BUF_RUN) generate
	@echo "Generating disco-agent Java code (using Docker)..."
	$(BUF_RUN) generate --template buf.gen.agent.yaml --path proto/discopanel/agent
	@echo "Proto generation complete!"

proto-clean:
	@echo "Cleaning generated proto files..."
	rm -rf pkg/proto
	rm -rf web/discopanel/src/lib/proto
	rm -rf agent/src/generated/java
	@echo "Proto files cleaned!"

proto-lint:
	@echo "Linting proto files (using Docker)..."
	$(BUF_RUN) lint || echo "Buf linting failed, but it's probably just missing comment documentation. Ignore it."
	@echo "Proto linting complete!"

gen: proto-clean proto

proto-format:
	@echo "Formatting proto files (using Docker)..."
	$(BUF_RUN) format -w
	@echo "Proto files formatted!"

proto-breaking:
	@echo "Checking for breaking changes (using Docker)..."
	$(BUF_RUN) breaking --against '.git#branch=main'
	@echo "Breaking change check complete!"

# proto-install:
# 	go install github.com/sudorandom/protoc-gen-connect-openapi@latest

# Help
help:
	@echo "Available commands:"
	@echo "  make dev            - Run in development mode (frontend + backend)"
	@echo "  make build          - Build standalone binary with embedded frontend"
	@echo "  make prod           - Build and run in production mode"
	@echo "  make image          - Build and push Docker image to :dev tag"
	@echo "  make dev-docker     - Build and run Docker container locally (no cache)"
	@echo "  make dev-auth       - Build and run with OIDC provider (Keycloak)"
	@echo "  make images         - Build runtime + module images locally when stale"
	@echo "  make runtime        - Build all runtime image variants locally"
	@echo "  make modules        - Build all module images locally"
	@echo "  make clean          - Remove data directory and build artifacts"
	@echo "  make kill-dev       - Kill any orphaned dev processes"
	@echo "  make deps           - Install all dependencies"
	@echo "  make test           - Run tests"
	@echo "  make fmt            - Format code"
	@echo "  make lint           - Lint code"
	@echo "  make check          - Type check frontend"
	@echo "  make gen            - Clean and regenerate proto code (via Docker)"
	@echo "  make proto          - Generate Go and TypeScript code from proto files (via Docker)"
	@echo "  make proto-clean    - Remove all generated proto files"
	@echo "  make proto-lint     - Lint proto files for style and correctness (via Docker)"
	@echo "  make proto-format   - Format proto files (via Docker)"
	@echo "  make proto-breaking - Check for breaking changes against main (via Docker)"
	@echo "  make help           - Show this help message"