# 08 — Roadmap & Milestones

Phased delivery. Each milestone is independently demoable and ends with the relevant
[test book](07-test-book.md) gates green.

> **Lightweight revamp (milestone R, after M1).** The stack was slimmed: Authentik → local
> email/password accounts (argon2id) with open signup and API-issued HS256 tokens;
> OpenTelemetry/collector/Grafana overlay removed (structured `slog` logs + health
> endpoints only); Caddy removed (TLS is the host proxy's job); published-version
> snapshots dropped (re-publish overwrites). Groups/ACL, PATs, audit log, Meilisearch,
> Kroki, Garage and Valkey stay. Items below that mention the removed pieces are
> rewritten accordingly; M0/M1 describe what was originally built.

## M0 — Foundations & walking skeleton (infra up, one flow end-to-end)
**Goal:** `docker compose up` brings the whole stack; a hardcoded user can create a draft and read it.
- Repo scaffolding: Go API (`fiber`, `sqlc`, `pgx`, `goose`), SvelteKit app, Docker Compose.
- Services wired: Postgres, Meilisearch, Kroki, Garage, Authentik, Caddy, OTel Collector.
- OIDC login via Authentik (AC-1.x).
- DB schema v1 + migrations ([03](03-data-model.md)).
- **Observability from day one:** OpenTelemetry SDK wired (traces/metrics/logs → OTLP), coherent `slog` JSON logger trace-correlated across the service, `otelfiber`/`otelpgx` instrumentation, opt-in `docker-compose.observability.yml` (Grafana + Prometheus + Tempo + Loki).
- Health/readiness endpoints; CI skeleton (build/test/lint/scan).
- **Exit:** login works; create+save draft; read own draft; CI green.

## M1 — Authoring & publish workflow (P0 core)
**Goal:** the draft → publish lifecycle with metadata gate and versioning.
- CodeMirror 6 editor with live preview (F3 basics: md, code).
- Draft save + autosave + private workspace (F2).
- Metadata model + publish confirm + validation gate (F5, F6).
- Published-version snapshots (F9 storage; UI minimal).
- Server-side render pipeline: goldmark + bluemonday; store rendered HTML (F8 read).
- **Exit:** author drafts, publishes with valid metadata, reads rendered doc; T-2._, T-5._, T-8.1/8.5 pass.

## R — Lightweight revamp
**Goal:** the simplest, lightest stack that still delivers the product.
- Remove observability (OTel SDK, collector, overlay); plain `slog`.
- Local email/password auth: `users.password_hash` (argon2id), signup/login API, HS256 tokens, per-IP rate limit (Valkey).
- Login/signup pages in the web BFF; encrypted session cookie holds the API token.
- Drop `document_versions` snapshots.
- Slim Compose/Makefile/CI: no Caddy, no Authentik, no profiles or overlays.
- Local groups + membership (`group_members`), admin management surface.
- **Exit:** `make up` brings 8 services; signup → draft → publish → read works end-to-end; unit, integration and e2e suites green.

## M2 — Search & indexing (the focus)
**Goal:** instant, access-agnostic search working end-to-end (public docs).
- Meilisearch index schema + settings ([05](05-search-and-indexing.md)).
- Outbox indexing pipeline (publish/unpublish/edit → index).
- `/api/search` proxy with snippets/highlighting/facets (F7, minus ACL filter).
- Search UI (minimal, as-you-type).
- `admin/reindex`.
- **Exit:** typo-tolerant faceted search over 100k seeded docs; PERF-1 met; T-7.1/7.2/7.3/7.5 pass.

## M3 — Access control & groups (P0)
**Goal:** restricted docs + group scoping, enforced everywhere.
- Local groups + membership management (F10).
- Document ACL model + restrict UI (F10).
- Access-filter injection in search; ACL facets; attachment brokering (F4, F7.4/7.7).
- Audit log for publish/unpublish/ACL/admin.
- **Public read API core** ([10](10-public-api.md)): `/api/v1` search/list/retrieve/tags reusing the *same* access-filtered path; token auth (PAT verification); per-token rate limiting. Built here so the API is access-correct from birth (F12 core: T-12.1–12.8).
- **Exit:** SEC-2 access-control matrix (UI **and** API) + T-7.4/7.7/8.3/10.2 + T-12.3/12.6 all pass (**zero leakage**).

## M4 — Technical rendering completeness
**Goal:** all required Markdown extensions production-quality.
- Kroki integration at publish: Mermaid, PlantUML/UML, GraphViz (+ inline sanitized SVG).
- KaTeX + mhchem; SmilesDrawer (F3.3–3.5).
- Attachments/images via Garage (F4 full).
- Editor previews for all block types.
- **Exit:** T-3.3/3.4/3.5, T-4.x pass; SEC-1 XSS corpus + SEC-4 SSRF pass.

## M5 — Admin & polish
**Goal:** production readiness.
- Admin: metadata-limit settings, unpublish/moderate, group management surface (F10 UI).
- **Public API finish** ([10](10-public-api.md)): PAT management UI (create/scope/revoke), OpenAPI 3 spec + `/api/docs`, content negotiation polish, ETag/304, generated SDKs (Go/TS/Python) in CI (F12: T-12.9/12.10/12.11).
- Accessibility pass (NFR-4), keyboard shortcuts, empty/error states, minimal-chrome polish.
- Perf hardening (caching, cold-read budget).
- **Exit:** all P0/P1 ACs green; perf & security gates met.

## M6 — Hardening, docs & OSS release
**Goal:** v1.0 public release.
- Full security pass: DAST (ZAP), pentest checklist, container hardening verified, CSP finalized.
- Backup/restore runbook + DR drill (T-11.x).
- User docs, admin/ops docs, `SECURITY.md`, `CONTRIBUTING.md`, license finalized ([09](09-open-questions.md)).
- Signed images (cosign), SBOM, pinned digests, release automation.
- **Exit:** clean `docker compose up` in < 5 min; all test-book gates green; v1.0 tagged.

## Post-v1 candidates (see [09](09-open-questions.md))
- Real-time multiplayer (Yjs) — data model already single-author-friendly.
- Published-version history (list/diff/restore) and per-save draft history.
- MFA / external SSO (OIDC) as an optional add-on.
- Comments/annotations.
- Multi-tenant / org isolation.
- Related-document suggestions (embeddings / semantic search via Meilisearch vector search).
- Postgres FTS degraded-mode fallback.
- Import/export (bulk Markdown, migration from Confluence/Notion/Obsidian vaults).
- **API write operations** (create/update/publish via API) + GraphQL endpoint + outbound webhooks on publish/ACL change ([10](10-public-api.md) §9, [09](09-open-questions.md)).

## Rough sequencing note
M0→M3 form the true MVP (author, publish, search, secure access). M4 makes it
technically complete; M5–M6 make it releasable. Search (M2) and access control (M3) are
the highest-risk/highest-value; prioritize their tests early.
