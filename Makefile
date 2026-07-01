.PHONY: help run build test lint swag migrate migrate-up migrate-down migrate-status up down logs

API_DIR := api

help:
	@echo "Usage: make <target>"
	@echo ""
	@echo "  run      Start the API server (go run)"
	@echo "  build    Compile the API binary to dist/"
	@echo "  test     Run all Go tests with race detector"
	@echo "  lint     Run golangci-lint on the API"
	@echo "  swag     Regenerate Swagger docs from handler annotations"
	@echo "  migrate-up      Apply all pending migrations"
	@echo "  migrate-down    Roll back the last migration"
	@echo "  migrate-status  Show migration status"
	@echo "  migrate CMD=<cmd>  Run a specific goose command"
	@echo "  up       Start infrastructure services (postgres, qdrant, ollama)"
	@echo "  down     Stop infrastructure services"
	@echo "  logs     Tail logs from all infrastructure services"

run:
	cd $(API_DIR) && go run ./cmd/server

build:
	mkdir -p dist
	cd $(API_DIR) && go build -o ../dist/neuralvault ./cmd/server

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

down:
	docker compose down

logs:
	docker compose logs -f
