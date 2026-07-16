# LookupKB developer task runner.
# Run `make help` for the list of targets.

SHELL := /bin/bash
COMPOSE := docker compose -f deploy/docker-compose.yml --env-file .env
COMPOSE_OBS := docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.observability.yml --env-file .env
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

.PHONY: auth-up
auth-up: env ## Start the core stack + Authentik (--profile auth)
	$(COMPOSE) --profile auth up -d --build

.PHONY: obs-up
obs-up: env ## Start the core stack + observability overlay (Grafana at /grafana)
	$(COMPOSE_OBS) up -d --build

.PHONY: obs-down
obs-down: ## Stop the core stack + observability overlay
	$(COMPOSE_OBS) down

.PHONY: garage-init
garage-init: ## Assign the initial Garage cluster layout (run once after `make up`)
	@NODE_ID=$$($(COMPOSE) exec -T garage /garage node id -q 2>/dev/null | head -n1 | cut -d@ -f1); \
	test -n "$$NODE_ID" || { echo "Could not read Garage node id — is the garage service up?"; exit 1; }; \
	echo "Assigning layout to node $$NODE_ID"; \
	$(COMPOSE) exec -T garage /garage layout assign "$$NODE_ID" -z dc1 -c 1G; \
	$(COMPOSE) exec -T garage /garage layout apply --version 1; \
	echo "Garage layout applied. Create the attachments bucket + an app key with:"; \
	echo "  $(COMPOSE) exec garage /garage bucket create attachments"; \
	echo "  $(COMPOSE) exec garage /garage key create lookupkb-app   # paste keys into .env"

.PHONY: config
config: env ## Validate the core + observability Compose configs
	$(COMPOSE) config -q && echo "core compose config OK"
	$(COMPOSE_OBS) config -q && echo "observability overlay config OK"

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
