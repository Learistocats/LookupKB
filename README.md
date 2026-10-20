# LookupKB

[![CI](https://github.com/Learistocats/LookupKB/actions/workflows/ci.yml/badge.svg)](https://github.com/Learistocats/LookupKB/actions/workflows/ci.yml)

A **fast, minimal knowledge base**. Authors write Markdown in an Obsidian-like
editor; documents are published into a flat, fully-indexed corpus that anyone
(subject to access rules) can find in milliseconds. **No folders, no file tree** —
organization is emergent, via metadata (tags) and search.

> Product bet: *organization by search + metadata beats organization by hierarchy*
> at scale, and **UX speed is the feature**.

Contributions are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md). Security issues:
see [SECURITY.md](SECURITY.md).

## Stack

| Layer | Tech |
|---|---|
| Backend | Go 1.26 · [Fiber v2](https://gofiber.io) · `slog` · `caarlos0/env` |
| Frontend | SvelteKit · TypeScript · Vite |
| Data | PostgreSQL · Meilisearch · Garage (S3) · Kroki (diagrams) · Valkey (rate limits) |
| Identity | Built-in email + password (argon2id), open signup |

## Repository layout

```
api/     Go + Fiber backend (cmd/api, internal/*)
web/     SvelteKit frontend
deploy/  docker-compose stack, service configs
docs/    deployment & auth guides
```

## Quickstart

Prerequisites: **Docker** + **Docker Compose v2**.

```bash
cp .env.example .env      # then edit secrets (or `make env`)
make up                   # build + start the stack (detached)
make garage-init          # one-time: assign the Garage cluster layout
```

Then:

- App: <http://localhost:3000> — create an account at `/signup`, then search from the
  home page or press **+** to write an entry; your entries live in the panel on the left
- API liveness: <http://127.0.0.1:8080/healthz>
- API readiness: <http://127.0.0.1:8080/readyz>

Stop it with `make down` (add `make clean` to also drop volumes).

`make up` starts web, API, Postgres, Valkey, Meilisearch, Kroki and Garage with
healthchecks gating an ordered start. No reverse proxy is bundled — put your own
TLS proxy in front of `web` for anything deployed.

See [`docs/deploy.md`](docs/deploy.md) for the full bring-up, health, network
boundary, and configuration reference, and [`docs/auth.md`](docs/auth.md) for
accounts and sessions.

## Local development (without containers)

Run the API and web dev server directly for fast iteration:

```bash
# terminal 1 — API on :8080
cd api && go run ./cmd/api

# terminal 2 — SvelteKit dev server on :5173 (proxies /healthz, /api → :8080)
cd web && pnpm install && pnpm dev
```

Open <http://localhost:5173>. Without `LOOKUPKB_AUTH_SECRET` (API) and
`SESSION_COOKIE_SECRET` (web) auth is disabled and protected routes don't exist;
with them (plus a database) you can sign up locally.

## Common tasks

```bash
make help     # list all targets
make build    # build API binary + web assets
make test     # go test ./...  +  web unit tests
make lint     # golangci-lint + go vet  +  eslint + svelte-check
make fmt      # format Go + web sources
make config   # validate the compose file
```

## Continuous integration & releases

Every pull request and every push to `develop`/`main` runs
[`.github/workflows/ci.yml`](.github/workflows/ci.yml) as parallel jobs:

| Job | What it checks |
|---|---|
| `PR title` | Conventional Commits PR title (PRs are squash-merged; the title feeds the changelog) |
| `API (Go)` | `go mod tidy`, `go vet`, `golangci-lint` (govet/staticcheck/**gosec**), sqlc freshness, `go test -race`, testcontainers integration tests, build |
| `Govulncheck (Go)` | Go dependency/stdlib vulnerabilities |
| `Web (SvelteKit)` | `pnpm audit`, Prettier, ESLint, `svelte-check`, Vitest, build |
| `Compose config` | `docker compose config` validation |
| `Trivy (IaC/config)` | Trivy misconfiguration scan of Dockerfiles + compose |
| `Build images` | Build the API & web images, then Trivy image scans |

`pnpm audit`, Trivy and `govulncheck` **fail the build** on HIGH/CRITICAL findings;
accepted exceptions are documented in [`.trivyignore`](.trivyignore).

**Releases** are cut by [release-please](https://github.com/googleapis/release-please)
([`.github/workflows/release.yml`](.github/workflows/release.yml)): merging `develop`
into `main` updates a release PR; merging that PR tags `vX.Y.Z` and publishes

- multi-arch (`amd64`/`arm64`) images `ghcr.io/learistocats/lookupkb-{api,web}`,
  cosign-signed with SBOM + provenance attestations;
- a deploy bundle (`lookupkb-vX.Y.Z-deploy.tar.gz`: compose file pinned to the
  release images, `.env.example`, service configs) attached to the GitHub Release.

To run a release: download the bundle, `cp .env.example .env`, set the secrets, then
`docker compose up -d`.

## Security & operations notes

- The API image is a **distroless, non-root, read-only-filesystem** static
  binary with a self-probe healthcheck (no shell in the image).
- Passwords are stored as **argon2id** hashes only; login/signup are rate-limited.
- Compose uses **network segmentation**: only `web` is published (the API binds to
  loopback); datastores live on an `internal` network. `no-new-privileges`, dropped capabilities and
  per-container resource limits are set across the stack.
- All third-party images are **pinned by digest**.
- **No secrets in git**: `.env` is git-ignored; `.env.example` documents every
  variable.

## License

[Apache-2.0](LICENSE).
