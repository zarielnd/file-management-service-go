.PHONY: all build test test-race fmt vet proto \
        docker-up docker-up-build docker-down docker-down-volumes \
        docker-build clean

SERVICES := file-server storage

# =============================================================================
# Build
# =============================================================================

build: ## Build all service binaries into bin/
	@mkdir -p bin
	go build -o bin/file-server ./services/file-server/cmd
	go build -o bin/storage ./services/storage/cmd

# =============================================================================
# Development
# =============================================================================

MODULES := gen services/file-server services/storage


test: ## Run tests for all modules
	@for %%m in ($(MODULES)) do ( \
		echo === Testing %%m === && \
		go -C %%m test ./... \
	)

test-race: ## Run tests with race detector for all modules
	@for %%m in ($(MODULES)) do ( \
		echo === Race testing %%m === && \
		go -C %%m test -race ./... \
	)

test-cover: ## Run tests with coverage for all modules
	@for %%m in ($(MODULES)) do ( \
		echo === Coverage testing %%m === && \
		go -C %%m test -cover ./... \
	)

fmt: ## Format all Go code
	@for %%m in ($(MODULES)) do ( \
		echo === Formatting %%m === && \
		go -C %%m fmt ./... \
	)

vet: ## Run go vet on all modules
	@for %%m in ($(MODULES)) do ( \
		echo === Vetting %%m === && \
		go -C %%m vet ./... \
	)

lint: ## Run golangci-lint
	golangci-lint run ./gen/... ./services/file-server/... ./services/storage/...

# =============================================================================
# Code Generation
# =============================================================================

proto: ## Generate Go code from protobuf definitions
	protoc \
		--proto_path=proto \
		--go_out=gen \
		--go_opt=paths=source_relative \
		--go-grpc_out=gen \
		--go-grpc_opt=paths=source_relative \
		storage/v2/storage.proto

# =============================================================================
# Docker
# =============================================================================

docker-up: ## Start docker compose
	docker compose up

docker-up-build: ## Build and start docker compose
	docker compose up --build

docker-down: ## Stop docker compose
	docker compose down

docker-down-volumes: ## Stop and remove volumes
	docker compose down -v

docker-build: ## Build docker images
	docker compose build

# =============================================================================
# Cleanup
# =============================================================================

clean: ## Remove build artifacts
	rm -rf bin/