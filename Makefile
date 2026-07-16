# LookupKB developer task runner.
# Run `make help` for the list of targets.

SHELL := /bin/bash
COMPOSE := docker compose -f deploy/docker-compose.yml --env-file .env
API_DIR := api
WEB_DIR := web

.DEFAULT_GOAL := help

## ── Meta ─────────────────────────────────────────────────────────────────────

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: env
env: ## Create .env from .env.example if missing
	@test -f .env || (cp .env.example .env && echo "Created .env from .env.example — edit secrets before deploying")

## ── Stack (Docker Compose) ───────────────────────────────────────────────────

.PHONY: up
up: env ## Build and start the core stack (detached)
	$(COMPOSE) up -d --build

.PHONY: dev
dev: env ## Build and start the core stack in the foreground (logs attached)
	$(COMPOSE) up --build

.PHONY: down
down: ## Stop the stack
	$(COMPOSE) down

.PHONY: clean
clean: ## Stop the stack and remove volumes (DESTRUCTIVE)
	$(COMPOSE) down -v

.PHONY: logs
logs: ## Tail stack logs
	$(COMPOSE) logs -f

.PHONY: config
config: env ## Validate the Compose configuration
	$(COMPOSE) config -q && echo "compose config OK"

## ── Build ────────────────────────────────────────────────────────────────────

.PHONY: build
build: build-api build-web ## Build API binary and web assets

.PHONY: build-api
build-api: ## Build the API binary into api/bin/
	cd $(API_DIR) && go build -o bin/api ./cmd/api

.PHONY: build-web
build-web: ## Build the web static assets
	cd $(WEB_DIR) && pnpm install --frozen-lockfile && pnpm run build

## ── Test ─────────────────────────────────────────────────────────────────────

.PHONY: test
test: test-api test-web ## Run all tests

.PHONY: test-api
test-api: ## Run Go tests
	cd $(API_DIR) && go test ./...

.PHONY: test-web
test-web: ## Run web unit tests
	cd $(WEB_DIR) && pnpm install --frozen-lockfile && pnpm run test

## ── Lint / format ────────────────────────────────────────────────────────────

.PHONY: lint
lint: lint-api lint-web ## Lint everything

.PHONY: lint-api
lint-api: ## Lint Go (golangci-lint) + go vet
	cd $(API_DIR) && go vet ./... && golangci-lint run

.PHONY: lint-web
lint-web: ## Lint + type-check the web app
	cd $(WEB_DIR) && pnpm install --frozen-lockfile && pnpm run lint && pnpm run check

.PHONY: fmt
fmt: ## Format Go and web sources
	cd $(API_DIR) && go fmt ./...
	cd $(WEB_DIR) && pnpm run format
