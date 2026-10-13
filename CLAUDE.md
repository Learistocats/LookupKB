# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

LookupKB is a fast, minimal knowledge base: authors write Markdown in an
Obsidian-like editor and publish into a **flat, fully-indexed corpus** — no
folders, no file tree; organization is emergent via tags + search. Monorepo:

- `api/` — Go 1.25 + Fiber v2 backend (stateless JWT-verifying API)
- `web/` — SvelteKit + TypeScript frontend, acting as a **BFF** (owns the auth session)
- `deploy/` — Docker Compose stack (single file, no profiles/overlays) + per-service configs
- `docs/planning/` — the specs (see below); the source of truth for all behavior

## Specs and tickets drive the work

`docs/planning/*.md` are the authoritative specs. Work is planned as Linear
tickets `LOO-N` grouped into milestones **M0–M6** (`08-roadmap.md`); commits and
branches are tagged `[LOO-N]` / `task/LOO-N`. Ticket scope references acceptance
criteria by number (`AC-2.1`) defined in `04-functional-spec.md`, and points
back to specific spec sections (e.g. `03-data-model.md §1`). **Before implementing
a feature, read the ticket's referenced spec sections** — they encode hard product
rules (below) that the code is expected to honor exactly.

Milestone boundaries are enforced *by construction*, not just convention: e.g. the
M0 draft slice deliberately writes no `index_outbox` rows and never flips `status` to
`published`, because those are later milestones.

**Lightweight revamp (milestone R):** the stack was deliberately slimmed — no Authentik
(local email/password accounts), no OpenTelemetry/collector/Grafana, no Caddy, no
`document_versions` snapshots. Don't reintroduce any of these without being asked; older
specs/tickets that mention them are superseded (see `08-roadmap.md`).

## Common commands

Everything is driven through the `Makefile` (`make help` lists all targets):

```bash
make up            # build + start the stack (detached); web on :3000, API on 127.0.0.1:8080
make down          # stop the stack
make clean         # down + drop volumes (DESTRUCTIVE)

make test          # go test ./...  +  web vitest
make lint          # go vet + golangci-lint  +  eslint + svelte-check
make build         # API binary (api/bin/api) + web assets
make sqlc          # regenerate SQL bindings after editing queries (see below)
make migrate       # apply DB migrations
make db-reset      # prove migrations apply up+down on a throwaway PG (testcontainers)
```

Run a single Go test:

```bash
cd api && go test ./internal/server -run TestCreateDraft_Returns201OwnedDraft
```

Integration tests are build-tagged and use testcontainers (need Docker):

```bash
cd api && go test -tags integration ./internal/db/...
```

Run one web test / the Playwright e2e (e2e skips unless `E2E_BASE_URL` + creds are set,
and runs against a **live** stack):

```bash
cd web && pnpm exec vitest run src/lib/api.test.ts
cd web && pnpm run test:e2e
```

Local dev without containers (fast iteration): API `cd api && go run ./cmd/api`
on `:8080`; web `cd web && pnpm dev` on `:5173` (proxies `/healthz`, `/api` → `:8080`).

## Architecture: things that span multiple files

### Progressive boot / deny-by-default wiring
The API boots even with an **empty `LOOKUPKB_DATABASE_URL` or `LOOKUPKB_AUTH_SECRET`**.
Each subsystem is wired only when its config is present, via functional options in
`api/cmd/api/main.go` → `server.New(...)`:
- no auth secret ⇒ `WithAuth` omitted ⇒ **no protected `/api` routes exist at all**
- no Postgres pool ⇒ `WithAccounts`/`WithDocuments` omitted ⇒ signup/login and draft routes not registered
- no Valkey URL ⇒ the signup/login rate limiter counts in process memory
- `registerRoutes` in `api/internal/server/server.go` only mounts a route when its
  option is set (`s.auth != nil`, `s.documents != nil`).

The web side mirrors this: `web/src/hooks.server.ts` skips the login gate entirely when
no `SESSION_COOKIE_SECRET` is configured, so `pnpm dev` works without a backend. **When adding a feature,
follow this pattern** — gate the new surface on its dependency; never assume a
dependency is present.

### Auth: local accounts, BFF owns the session, Go verifies its own tokens
- **The Go API owns credentials** (`api/internal/auth`, `server/accounts.go`): open
  signup + login at `POST /api/v1/auth/{signup,login}` (public, rate-limited per IP via a
  Valkey-backed Fiber limiter). Passwords are **argon2id** PHC hashes in `users.password_hash`;
  login failures are identical for unknown email vs wrong password (dummy-hash verify).
  It returns an **HS256 JWT** signed with `LOOKUPKB_AUTH_SECRET` (`sub` = users.id).
- The API stays a **stateless verifier** on protected routes: the middleware verifies
  the token, loads the caller's groups from `group_members` on every request, and
  exposes identity via `auth.IdentityFromCtx(c)`. It holds no session.
- **SvelteKit is the BFF**: `/login` and `/signup` form actions call the API and seal the
  token into an encrypted session cookie (`lkb_session.0/.1/…`, chunking kept as a
  safeguard, `web/src/lib/server/session.ts`). `hooks.server.ts` redirects to `/login`
  when there is no valid session (no refresh — expired ⇒ sign in again). The **token
  lives server-side only** (`event.locals.accessToken`); server routes proxy to the API
  with `apiFetch(locals.accessToken, …)`.
- The browser never talks to the API directly; the API port is loopback-bound in compose.

### Database: sqlc + goose, embedded in one binary
- Schema lives in `api/internal/db/migrations/*.sql` (goose). **Schema v1 already
  defines every table** (`documents`, `tags`, `document_tags`,
  `settings`, `document_acl`, `audit_log`, `index_outbox`, …) — feature tickets add
  *queries + handlers*, rarely migrations.
- Queries are hand-written in `api/internal/db/queries/*.sql` and compiled by
  **sqlc** (`api/sqlc.yaml`) into `api/internal/db/dbgen/`. After editing a query,
  run `make sqlc` and commit the generated output — **CI fails if `dbgen` is stale**.
- Handlers depend on a narrow interface (e.g. `DocumentStore` in
  `server/documents.go`), not `*dbgen.Queries` directly, so unit tests use a fake
  store with no database.
- The single `api` binary multiplexes subcommands because the distroless image has
  no shell: `api` (serve), `api migrate up|down|status`, `api healthcheck`,
  `api readycheck` (the last two are the container HEALTHCHECK self-probes).

### Ownership / access checks are server-side and must not leak existence
Owner-scoped queries filter by `author_id` in SQL; a cross-author or missing row
comes back as `pgx.ErrNoRows` → **404 with an identical body** for "not yours" and
"doesn't exist", so a probing user can't distinguish them. Never rely on the UI
hiding another user's content. This scaffolding is extended (not rewritten) by the
M3 group-ACL work — structure new access checks as an author-or-public branch.

### Search: outbox worker + server-side filter
`api/internal/search` holds a small hand-rolled Meilisearch client (no SDK — avoids a
forced Go bump), the outbox worker (started from `main.go` when Postgres + Meili are
configured) and the query service behind `GET /api/search`. Publish/unpublish only
enqueue `index_outbox` rows inside their transaction; the worker applies them. The
access filter is always built server-side from the verified caller. `body_text` comes
from the published `rendered_html`, never from the working copy.

### Web UI: design tokens + app shell
All colors/type come from tokens in `web/src/app.css` (light on `:root`, dark via
`prefers-color-scheme` and `[data-theme]`); never put literal colors in components —
the CodeMirror theme uses the same `var(--…)` tokens. Theme choice lives in the
`lkb_theme` cookie and is rendered into `<html data-theme>` by `hooks.server.ts`.
Signed-in pages live in the `routes/(app)` group: its layout loads the history panel
(`/api/documents/mine`, `depends('app:entries')` — call `invalidate('app:entries')`
after anything that changes an entry). Editor is `/entries/{id}`, reader `/docs/{slug}`.

### Health/readiness distinction
`/healthz` = liveness (process up, never touches deps). `/readyz` = readiness:
**Postgres/Valkey/Meilisearch/Kroki/Garage gate** it (503 if any fail). Probe errors are
logged server-side but the response body only says `ok`/`unavailable` (no internal
hostnames leaked).

### Logging (no telemetry stack)
One `slog` logger (`internal/logging`), JSON to stdout; `request_id` and `actor_id`
(internal user id, never email) are added from the request context. There is no
OpenTelemetry/metrics — don't add print-debugging, use the slog logger.

### Compose topology
One file, no profiles/overlays: web (only browser-facing port) + API (loopback-bound) +
one-shot migrate + Postgres + Valkey + Meilisearch + Kroki + Garage, on segmented
networks (datastores on an `internal` network). TLS/reverse proxy is the operator's job.
Prefer the Makefile targets over raw `docker compose`.

## Hard product rules to preserve (from the specs)

These are security/correctness invariants that later features must not violate:

- **Publish-time render, once, server-side.** Published Markdown is untrusted author
  content; it is rendered (goldmark) and sanitized (bluemonday, strict allowlist)
  exactly once at publish and cached in `documents.rendered_html`. The read path is a
  straight column fetch — **never parse/sanitize Markdown on read**.
- **Drafts never touch search.** Do not enqueue an `index_outbox` row for a draft;
  only published documents are indexed.
- **No version history.** Draft saves overwrite in place; a (re-)publish overwrites
  `rendered_html`/metadata inside the same transaction as the status transition.
- **Publish is a deliberate, metadata-gated action**, never a side effect of saving a
  draft. Draft saves apply no validation gate.

## Notes

- `.env` (git-ignored; `.env.example` documents every variable) drives both the stack
  and local runs. `LINEAR_PAT` for Linear API access is in the git-ignored `.linear`.
- CI (`.github/workflows/ci.yml`) runs on every PR and push to `main` as parallel
  jobs: `api` (vet, golangci-lint incl. gosec, sqlc-freshness, `go test -race`,
  integration, build), `vuln` (govulncheck), `web` (prettier/eslint/svelte-check/
  vitest/build), `compose` config validation, and Trivy IaC + image scans. Trivy and
  govulncheck **fail** on HIGH/CRITICAL. Playwright e2e is **not** in CI (manual,
  against a live stack).
