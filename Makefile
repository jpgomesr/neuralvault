.PHONY: help run build build-cli test lint swag migrate migrate-up migrate-down migrate-status run-cli up up-gpu down logs

API_DIR := api

help:
	@echo "Usage: make <target>"
	@echo ""
	@echo "  run      Start the API server (go run)"
	@echo "  build    Compile the API binary to dist/"
	@echo "  build-cli  Compile the CLI to dist/ as nv (falls back to dist/neuralvault if nv is already on PATH)"
	@echo "  run-cli  Run the CLI without building (go run); pass args via ARGS='ingest README.md'"
	@echo "  test     Run all Go tests with race detector"
	@echo "  lint     Run golangci-lint on the API"
	@echo "  swag     Regenerate Swagger docs from handler annotations"
	@echo "  migrate-up      Apply all pending migrations"
	@echo "  migrate-down    Roll back the last migration"
	@echo "  migrate-status  Show migration status"
	@echo "  migrate CMD=<cmd>  Run a specific goose command"
	@echo "  up       Start infrastructure services (postgres, qdrant, ollama) — CPU only"
	@echo "  up-gpu   Same as up, but with NVIDIA GPU passthrough for ollama (requires the NVIDIA Container Toolkit)"
	@echo "  down     Stop infrastructure services"
	@echo "  logs     Tail logs from all infrastructure services"

run:
	cd $(API_DIR) && go run ./cmd/server

build:
	mkdir -p dist
	cd $(API_DIR) && go build -o ../dist/neuralvault ./cmd/server

build-cli:
	mkdir -p dist
	cd $(API_DIR) && go build -o ../dist/neuralvault-cli ./cmd/cli
	@if command -v nv >/dev/null 2>&1; then \
		echo "nv already exists on PATH -- skipping alias, use ./dist/neuralvault-cli (or ./dist/neuralvault)"; \
		cp dist/neuralvault-cli dist/neuralvault; \
	else \
		cp dist/neuralvault-cli dist/nv; \
	fi

run-cli:
	cd $(API_DIR) && go run ./cmd/cli $(ARGS)

test:
	cd $(API_DIR) && go test ./... -race

lint:
	cd $(API_DIR) && golangci-lint run

swag:
	cd $(API_DIR) && go run github.com/swaggo/swag/cmd/swag@v1.16.6 init -g cmd/server/main.go

migrate-up:
	cd $(API_DIR) && go run ./cmd/migrate up

migrate-down:
	cd $(API_DIR) && go run ./cmd/migrate down

migrate-status:
	cd $(API_DIR) && go run ./cmd/migrate status

migrate:
	cd $(API_DIR) && go run ./cmd/migrate $(CMD)

up:
	docker compose up -d

up-gpu:
	docker compose -f docker-compose.yml -f docker-compose.gpu.yml up -d

down:
	docker compose down

logs:
	docker compose logs -f
