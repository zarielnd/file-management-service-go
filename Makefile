.PHONY: all build test test-race fmt vet proto \
        up up-build down down-v \
        d-build clean \
		build-all push-all



SERVICES := file-server storage

ifeq ($(OS),Windows_NT)
	RM := cmd /c del /Q /F
else
	RM := rm -f
endif

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

MODULES := ./gen/... ./services/file-server/... ./services/storage/...

test:
	go test $(MODULES)

test-race:
	go test -race $(MODULES)

test-cover:
	go test -cover $(MODULES)

vet:
	go vet $(MODULES)

lint: ## Run golangci-lint
	golangci-lint run ./gen/... ./services/file-server/... ./services/storage/...

# =============================================================================
# Code Generation
# =============================================================================

proto: ## Generate Go code from protobuf definitions
	-$(RM) gen\storage\v2\*_grpc.pb.go
	protoc \
		--proto_path=proto \
		--go_out=gen \
		--go_opt=paths=source_relative \
		--connect-go_out=gen \
		--connect-go_opt=paths=source_relative \
		storage/v2/storage.proto

# =============================================================================
# Docker
# =============================================================================

up: ## Start docker compose
	docker compose up -d

up-build: ## Build and start docker compose
	docker compose up --build

down: ## Stop docker compose
	docker compose down

down-v: ## Stop and remove volumes
	docker compose down -v

d-build: ## Build docker images
	docker compose build
# =============================================================================
# Cleanup
# =============================================================================

clean: ## Remove build artifacts
	rm -rf bin/

# ------------------------------------------------------------------------------
# Docker Build & Push Configuration
# ------------------------------------------------------------------------------
REGION     := asia-southeast1
PROJECT_ID ?= $(shell gcloud config get-value project 2>/dev/null)
REPOSITORY := file-management
PLATFORM   := linux/amd64

REGISTRY := $(REGION)-docker.pkg.dev/$(PROJECT_ID)/$(REPOSITORY)

# ------------------------------------------------------------------------------
# File Server
# ------------------------------------------------------------------------------
.PHONY: build-file-server push-file-server

build-file-server:
	docker buildx build \
		--platform $(PLATFORM) \
		-t $(REGISTRY)/file-server:latest \
		-f services/file-server/Dockerfile \
		.

push-file-server: build-file-server
	docker push $(REGISTRY)/file-server:latest

# ------------------------------------------------------------------------------
# Storage Service
# ------------------------------------------------------------------------------
.PHONY: build-storage push-storage

build-storage:
	docker buildx build \
		--platform $(PLATFORM) \
		-t $(REGISTRY)/storage-service:latest \
		-f services/storage/Dockerfile \
		.

push-storage: build-storage
	docker push $(REGISTRY)/storage-service:latest

# ------------------------------------------------------------------------------
# Temporal Worker
# ------------------------------------------------------------------------------
.PHONY: build-worker push-worker

build-worker:
	docker buildx build \
		--platform $(PLATFORM) \
		-t $(REGISTRY)/worker:latest \
		-f services/file-server/Dockerfile.worker \
		.

push-worker: build-worker
	docker push $(REGISTRY)/worker:latest

# ------------------------------------------------------------------------------
# All-in-one Targets
# ------------------------------------------------------------------------------
.PHONY: build-all push-all

build-all: build-file-server build-storage build-worker

push-all: push-file-server push-storage push-worker
	terraform -chdir=infra apply --auto-approve