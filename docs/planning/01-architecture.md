# 01 — System Architecture & Deployment

## 1. Component overview

```
                         ┌──────────────────────────────────────────┐
                         │                Browser                    │
                         │  SvelteKit UI + CodeMirror 6 editor       │
                         │  KaTeX/mhchem · SmilesDrawer (client)     │
                         └───────────────┬──────────────────────────┘
                                         │ HTTP(S) (TLS by the host's own proxy)
                              ┌──────────▼───────────┐
                              │  LookupKB web (BFF)  │  login/signup forms, encrypted
                              │  SvelteKit, Node     │  session cookie, security headers
                              └──────────┬───────────┘
                                         │ Bearer token (server-side only)
                         ┌───────────────▼─────────────────────────┐
                         │          LookupKB API (Go)               │
                         │  accounts · authz · publish workflow ·   │
                         │  render pipeline · search proxy · uploads│
                         └──┬────────┬─────────┬────────┬────────┬──┘
                            │        │         │        │        │
                  ┌─────────▼──┐ ┌───▼───┐ ┌───▼───┐ ┌──▼────┐ ┌─▼──────┐
                  │ PostgreSQL │ │ Meili │ │ Kroki │ │Garage │ │ Valkey │
                  │ users, docs│ │search │ │diagram│ │S3 blob│ │ rate-  │
                  │ acl, audit │ │ index │ │render │ │uploads│ │ limits │
                  └────────────┘ └───────┘ └───────┘ └───────┘ └────────┘
```

All boxes are containers in one `docker-compose.yml` — no identity provider, no
telemetry backend, no bundled reverse proxy. The only component that persists
authoritative state is **PostgreSQL** (+ **Garage** for blobs). Meilisearch is a
**derived index** that can be fully rebuilt from Postgres.

## 2. Responsibilities

| Component | Responsibility | Source of truth? |
|---|---|---|
| **SvelteKit web (BFF)** | UI, editor, client-side preview rendering, login/signup forms; holds the API token in an encrypted HttpOnly session cookie and proxies API calls server-side; sets security headers | No |
| **LookupKB API (Go)** | Email/password accounts (argon2id) + HS256 access tokens, AuthZ enforcement, publish/unpublish workflow, metadata validation, server-side sanitized render at publish, search proxy w/ access filters, upload brokering, audit logging, index sync, **versioned public read API** (`/api/v1`) for programmatic clients — same access-filtered path as the UI ([10](10-public-api.md)) | No (orchestrator) |
| **PostgreSQL** | Users (email + password hash), groups + membership, documents, drafts, tags, ACL, audit log | **Yes** |
| **Meilisearch** | Full-text + faceted search index | No (rebuildable) |
| **Kroki** | Server-side diagram rendering (Mermaid/PlantUML/GraphViz/etc.) → SVG | No |
| **Garage** | S3-compatible object storage for images/attachments | Yes (blobs) |
| **Valkey** | Shared login/signup rate-limit counters | No (ephemeral) |

## 3. Key request flows

### 3.1 Authentication
Local email/password accounts; there is no external identity provider.
1. **Signup** (open): the web `/signup` form posts to the BFF, which calls
   `POST /api/v1/auth/signup`. The API validates the email and password (≥ 12 chars),
   stores an **argon2id** hash, and returns an access token.
2. **Login**: `/login` → `POST /api/v1/auth/login`. Unknown email and wrong password return
   an identical 401 after identical argon2 work (no account enumeration via the response).
   Both routes are rate-limited per client IP (counters in Valkey).
3. The API's access token is an **HS256 JWT signed with `LOOKUPKB_AUTH_SECRET`** (`sub` =
   internal user id, `email`, `exp`; default TTL 7 days, no refresh). The BFF seals it into
   an encrypted HttpOnly cookie; the browser never sees it.
4. On each protected request the API verifies the token and loads the caller's **group
   membership from Postgres** (`group_members`), so ACL decisions always reflect current
   membership. Logout = the BFF drops the cookie (the API is stateless).

### 3.2 Read a published document (hot path — must be fast)
1. Browser requests `/api/documents/{slug}`.
2. API checks access: is doc `public`, or does the user's `groups` intersect the doc's ACL?
3. API returns **pre-rendered, sanitized HTML** (produced at publish time, cached) + metadata.
4. Client hydrates math/chemistry (KaTeX/SmilesDrawer run on the trusted HTML). Diagrams are already inlined as sanitized SVG from publish-time Kroki calls.

> Because published HTML is rendered and sanitized **at publish time** and cached (in
> Postgres and/or an in-memory/Redis-optional layer), reads are effectively static and
> extremely fast. No Markdown parsing or diagram rendering on the read path.

### 3.3 Search (hot path)
1. Browser sends the query to the web BFF (`/search`), which forwards it with the
   caller's token to `GET /api/search` (never directly to Meilisearch).
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
4. In one transaction: flips status, caches `rendered_html` (a re-publish overwrites it —
   there is no version history), applies tags, writes an audit-log entry and enqueues the
   Meilisearch upsert (outbox).
5. Returns the published document.

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
Single-host **Docker Compose**, one file, no profiles or overlays. Services:
`web`, `api` (+ one-shot `migrate`), `postgres`, `valkey`, `meilisearch`, `kroki`
(+ `kroki-mermaid`), `garage`. Only `web` is published to the network; the API port is
bound to loopback by default and is exposed only when the operator's own reverse proxy
routes the public `/api/v1` surface to it. TLS termination is the host proxy's job.

### 5.2 Images
- LookupKB API: multi-stage Go build → **distroless/static** final image, non-root, read-only FS, no shell.
- LookupKB web: SvelteKit `adapter-node` server, non-root.
- All third-party images pinned by digest; scanned with Trivy in CI.

### 5.3 Configuration
12-factor via env vars + a documented `.env.example`. Secrets via Docker secrets / env,
never baked into images.

### 5.4 Backups & DR
- **PostgreSQL**: nightly `pg_dump` / WAL archiving (authoritative).
- **Garage**: replicated buckets; snapshot to external S3 target.
- **Meilisearch**: not backed up — rebuilt from Postgres via a `reindex` admin command.
- Documented restore runbook; restore drill in CI is a stretch goal.

### 5.5 Scaling path (post-v1)
- LookupKB API is stateless → horizontal scale behind any load balancer (rate-limit
  counters are already shared through Valkey).
- Meilisearch single-node handles millions of docs; cluster if needed.
- Garage & Postgres scale via their own replication.

## 6. Operations

Deliberately minimal: no telemetry SDK or backend ships with LookupKB.
- **Logs:** one structured `slog` logger (JSON by default) on stdout, collected by the
  container runtime. Every line carries `service`, `version`, `request_id` and `actor_id`
  (internal user id, never the email). See [02](02-tech-stack.md) §6.
- **Health:** `/healthz` (liveness, never touches dependencies) and `/readyz` (gated on
  Postgres/Valkey/Meili/Kroki/Garage; 503 if any fail; the body only says `ok`/`unavailable`).
- **Audit:** business-level audit log persisted in Postgres (see [03](03-data-model.md)).
