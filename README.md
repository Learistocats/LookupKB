# LookupKB

[![CI](https://github.com/Learistocats/LookupKB/actions/workflows/ci.yml/badge.svg)](https://github.com/Learistocats/LookupKB/actions/workflows/ci.yml)

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
| Backend | Go 1.25 · [Fiber v2](https://gofiber.io) · `slog` · `caarlos0/env` |
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
make garage-init          # one-time: assign the Garage cluster layout
```

Then:

- App: <http://localhost> (SvelteKit SPA — shows live API health)
- API liveness: <http://localhost/healthz>
- API readiness: <http://localhost/readyz>

Stop it with `make down` (add `make clean` to also drop volumes).

The default `make up` starts the **core stack** (Caddy, API, web, Postgres,
Valkey, Meilisearch, Kroki, Garage, OTel Collector) with healthchecks gating an
ordered start. Identity and the bundled telemetry backend are opt-in:

```bash
make auth-up      # + Authentik (--profile auth)
make obs-up       # + Prometheus/Tempo/Loki/Grafana (Grafana at /grafana)
```

See [`docs/deploy.md`](docs/deploy.md) for the full bring-up, health, network
boundary, and configuration reference.

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

## Continuous integration

Every push to `main` and every pull request runs [`.github/workflows/ci.yml`](.github/workflows/ci.yml)
as parallel jobs:

| Job | What it checks |
|---|---|
| `api` | `go vet`, `golangci-lint` (govet/staticcheck/**gosec**), sqlc freshness, `go test -race`, testcontainers integration tests, build |
| `vuln` | `govulncheck` — Go dependency/stdlib vulnerabilities |
| `web` | Prettier, ESLint, `svelte-check`, Vitest, build |
| `compose` | `docker compose config` validation (core + observability overlay) |
| `trivy-config` | Trivy IaC/misconfiguration scan of Dockerfiles + compose |
| `images` | Build the API & web images, then Trivy image scans |

Trivy and `govulncheck` **fail the build** on HIGH/CRITICAL findings (not just warn);
accepted exceptions are documented in [`.trivyignore`](.trivyignore).

> **Branch protection (repo-settings — action required by a repo admin).** Enabling
> these jobs as *required status checks* on `main` (so a red pipeline blocks merge)
> is a GitHub repository setting that CI itself cannot configure. A repo admin
> should, under **Settings → Branches → Branch protection rules** for `main`,
> require the checks `api`, `vuln`, `web`, `compose`, `trivy-config`, and `images`,
> and require branches to be up to date before merging.

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
