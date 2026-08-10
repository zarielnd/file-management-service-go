.PHONY: all help build test test-race fmt vet lint proto \
        migrate migrate-down migrate-version \
        docker-up docker-up-build docker-down docker-down-volumes \
        docker-build clean

# Services matching your directory names under services/
SERVICES := file-server storage

# =============================================================================
# Development
# =============================================================================

all: fmt vet proto build test

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: ## Build service binaries into bin/
	@mkdir -p bin
	@for service in $(SERVICES); do \
		echo "Building $$service..."; \
		go build -o bin/$$service ./services/$$service/cmd/... ; \
	done

test: ## Run unit tests for all services
	@for service in $(SERVICES); do \
		echo "Testing $$service..."; \
		(cd services/$$service && go test ./...); \
	done

test-race: ## Run tests with race detector
	@for service in $(SERVICES); do \
		echo "Testing $$service with race detector..."; \
		(cd services/$$service && go test -race ./...); \
	done

fmt: ## Format Go code
	@for service in $(SERVICES); do \
		(cd services/$$service && go fmt ./...); \
	done

vet: ## Run go vet
	@for service in $(SERVICES); do \
		echo "Vetting $$service..."; \
		(cd services/$$service && go vet ./...); \
	done

lint: ## Run golangci-lint (install if missing: https://golangci-lint.run/)
	@for service in $(SERVICES); do \
		echo "Linting $$service..."; \
		(cd services/$$service && golangci-lint run ./...); \
	done

# =============================================================================
# Code Generation
# =============================================================================

proto: ## Generate Go code from protobuf definitions
	buf generate proto

# =============================================================================
# Database
# =============================================================================

migrate: ## Run migrations up
	./scripts/migrate.sh up

migrate-down: ## Run migrations down
	./scripts/migrate.sh down

migrate-version: ## Check migration version
	./scripts/migrate.sh version

# =============================================================================
# Docker
# =============================================================================

docker-up: ## Start docker compose in foreground
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