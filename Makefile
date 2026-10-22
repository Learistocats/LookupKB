# LookupKB developer task runner.
# Run `make help` for the list of targets.

SHELL := /bin/bash
COMPOSE := docker compose -f deploy/docker-compose.yml --env-file .env
API_DIR := api
WEB_DIR := web
WEB_DEPS := $(WEB_DIR)/node_modules/.stamp

# Reinstall only when the lockfile or manifest changes.
$(WEB_DEPS): $(WEB_DIR)/pnpm-lock.yaml $(WEB_DIR)/package.json
	cd $(WEB_DIR) && pnpm install --frozen-lockfile
	@touch $@

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
up: env ## Build and start the stack (detached)
	$(COMPOSE) up -d --build

.PHONY: dev
dev: env ## Build and start the stack in the foreground (logs attached)
	$(COMPOSE) up --build

.PHONY: down
down: ## Stop the stack
	$(COMPOSE) down --remove-orphans

.PHONY: clean
clean: ## Stop the stack and remove volumes (DESTRUCTIVE)
	$(COMPOSE) down -v --remove-orphans

.PHONY: logs
logs: ## Tail stack logs
	$(COMPOSE) logs -f

.PHONY: garage-init
garage-init: ## Assign the initial Garage cluster layout (run once after `make up`)
	@COMPOSE="$(COMPOSE)" deploy/garage-init.sh

define admin
@test -n "$(EMAIL)" || (echo "usage: make admin-$(1) EMAIL=you@example.com" && exit 2)
$(COMPOSE) exec api /api admin $(1) "$(EMAIL)"
endef

.PHONY: admin-grant
admin-grant: ## Make an account super-admin: make admin-grant EMAIL=you@example.com
	$(call admin,grant)

.PHONY: admin-revoke
admin-revoke: ## Remove super-admin: make admin-revoke EMAIL=you@example.com
	$(call admin,revoke)

.PHONY: config
config: env ## Validate the Compose config
	$(COMPOSE) config -q && echo "compose config OK"

## ── Build ────────────────────────────────────────────────────────────────────

.PHONY: build
build: build-api build-web ## Build API binary and web assets

.PHONY: build-api
build-api: ## Build the API binary into api/bin/
	cd $(API_DIR) && go build -o bin/api ./cmd/api

.PHONY: build-web
build-web: $(WEB_DEPS) ## Build the web app (adapter-node server)
	cd $(WEB_DIR) && pnpm run build

## ── Database ─────────────────────────────────────────────────────────────────

# Migrations connect as the DDL-owning migration role. Override to target a
# specific database; the default matches a locally-exposed dev Postgres.
MIGRATE_DATABASE_URL ?= postgres://lookupkb_migrator:change-me-lookupkb-migrator@localhost:5432/$(or $(POSTGRES_DB),lookupkb)?sslmode=disable
MIGRATE = cd $(API_DIR) && LOOKUPKB_MIGRATE_DATABASE_URL="$(MIGRATE_DATABASE_URL)" go run ./cmd/api migrate

.PHONY: migrate
migrate: ## Apply DB migrations (override MIGRATE_DATABASE_URL to target a DB)
	$(MIGRATE) up

.PHONY: migrate-down
migrate-down: ## Reverse the most recent migration
	$(MIGRATE) down

.PHONY: migrate-status
migrate-status: ## Show migration status
	$(MIGRATE) status

.PHONY: sqlc
sqlc: ## Regenerate type-safe SQL bindings (needs sqlc on PATH)
	cd $(API_DIR) && sqlc generate

.PHONY: db-reset
db-reset: ## Prove migrations apply up+down on a fresh throwaway Postgres (testcontainers)
	cd $(API_DIR) && go test -tags integration -run TestMigrations ./internal/db/...

## ── Test ─────────────────────────────────────────────────────────────────────

.PHONY: test
test: test-api test-web ## Run all tests

.PHONY: test-api
test-api: ## Run Go tests
	cd $(API_DIR) && go test ./...

.PHONY: test-web
test-web: $(WEB_DEPS) ## Run web unit tests
	cd $(WEB_DIR) && pnpm run test

## ── Lint / format ────────────────────────────────────────────────────────────

.PHONY: lint
lint: lint-api lint-web ## Lint everything

.PHONY: lint-api
lint-api: ## Lint Go (golangci-lint) + go vet
	cd $(API_DIR) && go vet ./... && golangci-lint run

.PHONY: lint-web
lint-web: $(WEB_DEPS) ## Lint + type-check the web app
	cd $(WEB_DIR) && pnpm run lint && pnpm run check

.PHONY: fmt
fmt: ## Format Go and web sources
	cd $(API_DIR) && go fmt ./...
	cd $(WEB_DIR) && pnpm run format
