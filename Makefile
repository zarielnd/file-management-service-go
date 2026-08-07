.PHONY: build test test-race fmt vet \
        proto migrate migrate-down migrate-version \
        docker-up docker-up-build docker-down docker-down-volumes \
        docker-build clean

SERVICES := file-server storage-service

build:
	@for service in $(SERVICES); do \
		echo "Building $$service..."; \
		cd services/$$service && go build ./...; \
		cd ../..; \
	done

test:
	@for service in $(SERVICES); do \
		echo "Testing $$service..."; \
		cd services/$$service && go test ./...; \
		cd ../..; \
	done

test-race:
	@for service in $(SERVICES); do \
		echo "Testing $$service with race detector..."; \
		cd services/$$service && go test -race ./...; \
		cd ../..; \
	done

fmt:
	@for service in $(SERVICES); do \
		cd services/$$service && go fmt ./...; \
		cd ../..; \
	done

vet:
	@for service in $(SERVICES); do \
		echo "Vetting $$service..."; \
		cd services/$$service && go vet ./...; \
		cd ../..; \
	done

proto:
	protoc \
		--go_out=. \
		--go-grpc_out=. \
		proto/storage/v1/storage.proto

migrate:
	./scripts/migrate.sh up

migrate-down:
	./scripts/migrate.sh down

migrate-version:
	./scripts/migrate.sh version

docker-up:
	docker compose up

docker-up-build:
	docker compose up --build

docker-down:
	docker compose down

docker-down-volumes:
	docker compose down -v

docker-build:
	docker compose build

clean:
	rm -rf bin/