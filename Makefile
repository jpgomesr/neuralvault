.PHONY: help run build test lint migrate up down logs

API_DIR := api

help:
	@echo "Usage: make <target>"
	@echo ""
	@echo "  run      Start the API server (go run)"
	@echo "  build    Compile the API binary to dist/"
	@echo "  test     Run all Go tests with race detector"
	@echo "  lint     Run golangci-lint on the API"
	@echo "  migrate  Run database migrations"
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

migrate:
	cd $(API_DIR) && go run ./cmd/migrate

up:
	docker compose up -d

down:
	docker compose down

logs:
	docker compose logs -f
