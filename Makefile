# Plinth starter-api — local-dev shortcuts.
# All the long-form commands are runnable directly; this Makefile is
# for muscle memory, not magic.

.PHONY: help up down run build test vet tidy migrate fmt

help:
	@echo "make up        - start postgres + cerbos via docker compose"
	@echo "make down      - stop the local stack"
	@echo "make run       - run the API directly (requires up)"
	@echo "make build     - go build ./cmd/server"
	@echo "make test      - go test ./..."
	@echo "make vet       - go vet ./..."
	@echo "make tidy      - go mod tidy"
	@echo "make migrate   - apply migrations (run after up)"
	@echo "make fmt       - gofmt -s -w"

up:
	docker compose up -d postgres cerbos

down:
	docker compose down

run:
	@SERVICE_NAME=$${SERVICE_NAME:-starter-api} \
	 SERVICE_VERSION=$${SERVICE_VERSION:-0.1.0-dev} \
	 MODULE_NAME=$${MODULE_NAME:-items} \
	 APP_ENV=$${APP_ENV:-dev} \
	 HTTP_ADDR=$${HTTP_ADDR::=:8080} \
	 DATABASE_URL=$${DATABASE_URL:-postgres://starter:starter@localhost:5432/starter?sslmode=disable} \
	 CERBOS_ADDRESS=$${CERBOS_ADDRESS:-localhost:3593} \
	 CERBOS_TLS=$${CERBOS_TLS:-false} \
	 AUDIT_MEMORY=$${AUDIT_MEMORY:-true} \
	 go run ./cmd/server

build:
	go build -o bin/server ./cmd/server

test:
	go test -race ./...

vet:
	go vet ./...

tidy:
	go mod tidy

migrate:
	@echo "Migrations are auto-applied by docker-compose's postgres init step."
	@echo "To re-run: docker compose down -v && docker compose up -d postgres"

fmt:
	gofmt -s -w .
