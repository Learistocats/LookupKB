# LookupKB

A **fast, minimal knowledge base**. Authors write Markdown in an Obsidian-like
editor; documents are published into a flat, fully-indexed corpus that anyone
(subject to access rules) can find in milliseconds. **No folders, no file tree** —
organization is emergent, via metadata (tags) and search.

> Product bet: *organization by search + metadata beats organization by hierarchy*
> at scale, and **UX speed is the feature**.

See [`docs/planning/`](docs/planning/) for the full specs (overview, architecture,
tech stack, data model, security, roadmap).

## Stack

| Layer | Tech |
|---|---|
| Backend | Go 1.23 · [Fiber v2](https://gofiber.io) · `slog` · `caarlos0/env` |
| Frontend | SvelteKit · TypeScript · Vite |
| Edge | Caddy (auto-TLS, security headers) |
| Data | PostgreSQL · Meilisearch · Garage (S3) · Kroki (diagrams) |
| Identity | Authentik (OIDC) |
| Telemetry | OpenTelemetry Collector |

## Repository layout

```
api/     Go + Fiber backend (cmd/api, internal/*)
web/     SvelteKit frontend
deploy/  docker-compose stack, Caddyfile, service configs
docs/    planning & specifications
```

## Quickstart

Prerequisites: **Docker** + **Docker Compose v2**.

```bash
cp .env.example .env      # then edit secrets (or `make env`)
make up                   # build + start the core stack (detached)
```

Then:

- App: <http://localhost> (SvelteKit SPA — shows live API health)
- API liveness: <http://localhost/healthz>
- API readiness: <http://localhost/readyz>

Stop it with `make down` (add `make clean` to also drop volumes).

The default `make up` starts the **core skeleton** (Caddy, API, web, Postgres,
Valkey, Meilisearch, Kroki, Garage). Heavier services are behind Compose
profiles until their wiring tickets land:

```bash
docker compose -f deploy/docker-compose.yml --env-file .env --profile auth up -d           # Authentik
docker compose -f deploy/docker-compose.yml --env-file .env --profile observability up -d   # OTel Collector
```

## Local development (without containers)

Run the API and web dev server directly for fast iteration:

```bash
# terminal 1 — API on :8080
cd api && go run ./cmd/api

# terminal 2 — SvelteKit dev server on :5173 (proxies /healthz, /api → :8080)
cd web && pnpm install && pnpm dev
```

Open <http://localhost:5173>.

## Common tasks

```bash
make help     # list all targets
make build    # build API binary + web assets
make test     # go test ./...  +  web unit tests
make lint     # golangci-lint + go vet  +  eslint + svelte-check
make fmt      # format Go + web sources
make config   # validate the compose file
```

## Security & operations notes

- The API image is a **distroless, non-root, read-only-filesystem** static
  binary with a self-probe healthcheck (no shell in the image).
- Compose uses **network segmentation**: only Caddy is published; datastores
  live on an `internal` network. `no-new-privileges`, dropped capabilities and
  per-container resource limits are set across the stack.
- All third-party images are **pinned by digest**.
- **No secrets in git**: `.env` is git-ignored; `.env.example` documents every
  variable.

## License

[Apache-2.0](LICENSE).
