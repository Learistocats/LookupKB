# 01 — System Architecture & Deployment

## 1. Component overview

```
                                   ┌──────────────────────────────────────────┐
                                   │                Browser                    │
                                   │  SvelteKit SPA + CodeMirror 6 editor      │
                                   │  KaTeX/mhchem · SmilesDrawer (client)     │
                                   └───────────────┬──────────────────────────┘
                                                   │ HTTPS
                                          ┌────────▼─────────┐
                                          │      Caddy       │  auto-TLS, security headers,
                                          │  (reverse proxy) │  rate-limit edge
                                          └───┬──────────┬───┘
                              /auth/*, OIDC   │          │  /api/*, /app/*
                                     ┌────────▼──┐   ┌───▼────────────────────────┐
                                     │ Authentik │   │      LookupKB API (Go)      │
                                     │  (OIDC)   │◄──┤  authz · publish workflow · │
                                     │ users,    │   │  render pipeline · search   │
                                     │ groups    │   │  proxy · uploads            │
                                     └───────────┘   └──┬─────┬───────┬────────┬───┘
                                                        │     │       │        │
                                          ┌─────────────▼┐ ┌──▼────┐ ┌▼──────┐ ┌▼────────┐
                                          │  PostgreSQL  │ │ Meili │ │ Kroki │ │ Garage  │
                                          │ documents,   │ │search │ │diagram│ │ S3 blobs│
                                          │ versions,    │ │ index │ │render │ │(uploads)│
                                          │ acl, audit   │ │       │ │       │ │         │
                                          └──────────────┘ └───────┘ └───────┘ └─────────┘
```

All boxes are containers in one `docker-compose.yml`. The only component that persists
authoritative state is **PostgreSQL** (+ **Garage** for blobs). Meilisearch is a
**derived index** that can be fully rebuilt from Postgres.

## 2. Responsibilities

| Component | Responsibility | Source of truth? |
|---|---|---|
| **SvelteKit SPA** | UI, editor, client-side preview rendering, calls API only | No |
| **Caddy** | TLS termination, HSTS/security headers, gzip/zstd, edge rate limiting, static asset serving | No |
| **LookupKB API (Go)** | AuthZ enforcement, publish/unpublish workflow, metadata validation, server-side sanitized render at publish, search proxy w/ access filters, upload brokering, audit logging, index sync, **versioned public read API** (`/api/v1`) for programmatic clients — same access-filtered path as the UI ([10](10-public-api.md)) | No (orchestrator) |
| **Authentik** | Authentication, users, groups, MFA, sessions/OIDC tokens | Yes (identity) |
| **PostgreSQL** | Documents, drafts, published versions, tags, ACL, audit log | **Yes** |
| **Meilisearch** | Full-text + faceted search index | No (rebuildable) |
| **Kroki** | Server-side diagram rendering (Mermaid/PlantUML/GraphViz/etc.) → SVG | No |
| **Garage** | S3-compatible object storage for images/attachments | Yes (blobs) |

## 3. Key request flows

### 3.1 Authentication
Browser → Caddy → Authentik (OIDC Authorization Code + PKCE). API validates the JWT
(issuer, audience, signature via Authentik JWKS), extracts `sub`, `email`, and `groups`.
No password ever touches LookupKB. Sessions are short-lived access tokens + refresh.

### 3.2 Read a published document (hot path — must be fast)
1. Browser requests `/api/documents/{slug}`.
2. API checks access: is doc `public`, or does the user's `groups` intersect the doc's ACL?
3. API returns **pre-rendered, sanitized HTML** (produced at publish time, cached) + metadata.
4. Client hydrates math/chemistry (KaTeX/SmilesDrawer run on the trusted HTML). Diagrams are already inlined as sanitized SVG from publish-time Kroki calls.

> Because published HTML is rendered and sanitized **at publish time** and cached (in
> Postgres and/or an in-memory/Redis-optional layer), reads are effectively static and
> extremely fast. No Markdown parsing or diagram rendering on the read path.

### 3.3 Search (hot path)
1. Browser sends query to `/api/search` (never directly to Meilisearch).
2. API injects an **access filter** derived from the caller's groups
   (`access = 'public' OR acl_groups IN [user's groups]`) and forwards to Meilisearch
   with a scoped, read-only key.
3. Meilisearch returns ranked, typo-tolerant, faceted results in single-digit–to-tens of ms.
4. API returns hits (title, description, tags, author, snippet, slug).

Meilisearch keys and host are **never** exposed to the browser. See [05](05-search-and-indexing.md).

### 3.4 Save draft
`PUT /api/drafts/{id}` — persists Markdown source + partial metadata to Postgres. No
validation gate. Private to author. Not indexed.

### 3.5 Publish (deliberate, confirmed)
1. `POST /api/documents/{id}/publish`.
2. API validates metadata (title/description/tags per limits). On failure → 422 with field errors.
3. API renders Markdown → sanitized HTML server-side (goldmark + bluemonday), sends diagram blocks to Kroki, inlines sanitized SVG.
4. Creates an immutable **version snapshot** (source + rendered HTML + metadata).
5. Upserts the document into Meilisearch with its ACL facets.
6. Writes an audit-log entry.
7. Returns the published document.

### 3.6 Upload attachment
`POST /api/uploads` (multipart) → API validates type/size, scans, stores in Garage under
a content-addressed key, returns a URL served back through the API (never a public bucket).

## 4. Rendering pipeline (security-critical)

| Stage | Where | Tech | Notes |
|---|---|---|---|
| Markdown → HTML | Server, at publish | `goldmark` (+GFM, footnotes) | Deterministic, no client trust |
| HTML sanitize | Server, at publish | `bluemonday` (strict allowlist) | Strips scripts/handlers; whitelists safe tags/attrs + KaTeX/SVG needs |
| Diagrams (Mermaid/PlantUML/UML/GraphViz) | Server, at publish | Kroki → SVG → sanitized | Rendered once, inlined; no client-side diagram JS on read path |
| Math & chem equations | Client, on render | KaTeX + mhchem | KaTeX is XSS-safe (`trust:false`); runs on already-sanitized HTML |
| Molecular structures (SMILES) | Client, on render | SmilesDrawer | Pure canvas/SVG drawing from a SMILES string |
| Editor live preview | Client, while editing | CodeMirror 6 decorations | Author previews *their own* content only |

Rationale: published content is untrusted-author input consumed by many readers, so it is
**rendered and sanitized server-side once** and cached. Only safe, XSS-hardened client
libs (KaTeX, SmilesDrawer) touch the read path. Diagram engines (Mermaid/PlantUML), which
are heavier and historically XSS-prone in-browser, run **server-side in Kroki** and never
in the reader's browser.

## 5. Deployment

### 5.1 Topology
Single-host **Docker Compose** for v1. Services:
`caddy`, `lookupkb-api`, `lookupkb-web` (or served by Caddy as static), `authentik-server`,
`authentik-worker`, `postgres`, `redis` (Authentik dependency; optionally LookupKB cache),
`meilisearch`, `kroki` (+ `kroki-mermaid`, `kroki-bpmn` companions as needed), `garage`,
`otel-collector` (receives OTLP from the API, fans out to the telemetry backend).
An **opt-in** `docker-compose.observability.yml` overlay adds Prometheus + Tempo/Jaeger +
Loki + Grafana for a batteries-included local backend; production deployments point the
collector at their existing telemetry backend instead.

### 5.2 Images
- LookupKB API: multi-stage Go build → **distroless/static** or `scratch` final image, non-root, read-only FS, no shell.
- LookupKB web: built static assets served by Caddy.
- All third-party images pinned by digest; scanned with Trivy in CI.

### 5.3 Configuration
12-factor via env vars + a documented `.env.example`. Secrets via Docker secrets / env,
never baked into images. TLS via Caddy automatic HTTPS (Let's Encrypt) or provided certs.

### 5.4 Backups & DR
- **PostgreSQL**: nightly `pg_dump` / WAL archiving (authoritative).
- **Garage**: replicated buckets; snapshot to external S3 target.
- **Meilisearch**: not backed up — rebuilt from Postgres via a `reindex` admin command.
- Documented restore runbook; restore drill in CI is a stretch goal.

### 5.5 Scaling path (post-v1)
- LookupKB API is stateless → horizontal scale behind Caddy.
- Meilisearch single-node handles millions of docs; cluster if needed.
- Garage & Postgres scale via their own replication.

## 6. Observability (OpenTelemetry-first)

LookupKB is instrumented with **OpenTelemetry** end-to-end so it is cloud-native by default
and so the performance acceptance criteria in [04](04-functional-spec.md)/[07](07-test-book.md)
are validated against real telemetry. The app emits **OTLP** (traces + metrics + logs) to a
local **OTel Collector**, which fans out to whatever backend the operator chooses
(vendor-neutral — no lock-in).

```
lookupkb-api ──OTLP (traces/metrics/logs)──► otel-collector ──► [ Prometheus | Tempo/Jaeger | Loki | vendor ]
                                                                         │
                                                                    Grafana (dashboards w/ AC budgets)
```

- **Traces:** every request produces an end-to-end trace; `otelfiber` instruments HTTP,
  `otelpgx` instruments DB, and hot-path operations (publish, server-render, Kroki call,
  Meilisearch query, Garage I/O) get explicit spans so latency is attributable per stage.
- **Metrics:** RED metrics per route plus the exact AC-tied instruments —
  `search_latency`, `read_latency`, `publish_total`, `index_lag_seconds`, `render_duration`.
  Grafana dashboards are preloaded with the p95 budgets from the test book so acceptance is
  a dashboard check, not a manual measurement.
- **Logs:** a single coherent structured logger for the whole image — `slog` JSON bridged
  to OTel (`otelslog`), so every log line is **trace-correlated** (`trace_id`/`span_id`) and
  shares one schema (`service`, `version`, `request_id`, `actor_id`, redacted PII). See
  [02](02-tech-stack.md) §6 for the whole-stack logging coherence approach.
- **Health:** `/healthz` (liveness) and `/readyz` (checks Postgres/Meili/Kroki/Garage/
  collector) endpoints. A Prometheus scrape endpoint remains available for non-OTLP setups.
- **Audit:** business-level audit log persisted in Postgres (see [03](03-data-model.md)),
  distinct from operational telemetry.
