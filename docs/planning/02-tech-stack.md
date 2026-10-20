# 02 — Technology Stack & Libraries

Guiding rule: **reuse proven OSS; write as little bespoke code as possible.** Every choice
below is open-source, self-hostable, and actively maintained (verify maintenance at
implementation time — see the "Maintenance watch" column).

## 1. Backend (Go)

| Concern | Library | Why | Maintenance watch |
|---|---|---|---|
| Web framework | `gofiber/fiber` (v2) | Express-like DX, very fast (built on `fasthttp`), batteries-included middleware | Active, large community |
| DB driver | `jackc/pgx` (v5) | Fastest, most complete Postgres driver | Active |
| Type-safe SQL | `sqlc` | Generates Go from SQL — no ORM runtime, compile-time safety | Active |
| Migrations | `pressly/goose` **or** `golang-migrate` | Versioned schema migrations | Active |
| Passwords | `golang.org/x/crypto/argon2` (argon2id) | Memory-hard password hashing, PHC-encoded, OWASP parameters | Go team |
| Tokens | `go-jose/go-jose` v4 (HS256 JWT) | API-issued access tokens, verified locally (no IdP) | Active |
| Markdown | `yuin/goldmark` (+GFM, footnotes, attributes) | CommonMark-compliant, extensible, safe, fast | Active, de-facto Go standard |
| HTML sanitizer | `microcosm-cc/bluemonday` | Allowlist HTML sanitization (XSS defense) | Active |
| Search client | `meilisearch/meilisearch-go` | Official Meilisearch SDK | Active |
| S3 client | `aws/aws-sdk-go-v2` (S3) | Works against Garage's S3 API | Active |
| Validation | `go-playground/validator` | Struct/field validation for metadata gates | Active |
| Config | `caarlos0/env` (or `spf13/viper`) | 12-factor env config | Active |
| Logging | `log/slog` (stdlib) | Structured JSON logging, no extra dependency | Stdlib |
| Rate limiting | `gofiber/fiber` limiter middleware + `redis/go-redis` (Valkey storage) | Login/signup abuse protection, counters shared across restarts | Active |
| Testing | `stretchr/testify`, `testcontainers-go` | Assertions + real Postgres/Meili in integration tests | Active |

**Framework decision:** **Fiber (v2)** for its Express-like ergonomics, built-in
middleware (limiter, CORS, request-id, recover, healthcheck), and speed. Caveat: Fiber is
built on `fasthttp`, not `net/http`, so it is **not** wire-compatible with the `net/http`
middleware ecosystem. This is acceptable here — our sensitive integrations don't need it:
token verification works on token *strings* (no `http.Request` dependency), and
`meilisearch-go` / `aws-sdk-go-v2` / `pgx` are outbound clients. The API sits behind the
web BFF / the operator's TLS-terminating proxy (HTTP/1.1 upstream), so fasthttp's lack of
native HTTP/2 is a non-issue. **Fallback:** `labstack/echo` (net/http)
if we ever hit an ecosystem wall — the handler layer is thin enough to swap.

## 2. Frontend (SvelteKit)

| Concern | Library | Why |
|---|---|---|
| App framework | **SvelteKit** (+ Vite, TypeScript) | Compiles away → tiny/fast runtime; fits minimal-UI, near-instant UX goal |
| Editor | **CodeMirror 6** (`@codemirror/*`, `@codemirror/lang-markdown`) | Obsidian-like live-preview via decorations; fast, extensible, accessible |
| Math | **KaTeX** + `mhchem` extension | Fast, synchronous, XSS-safe math + chemical equations |
| Molecules | **SmilesDrawer** | Renders SMILES strings to 2D structures client-side |
| Syntax highlight | **Shiki** (build/publish time) or `highlight.js` | Shiki gives editor-grade highlighting; can run server-side at publish |
| Search UI | Custom minimal component over `/api/search` | No InstantSearch dependency; keeps Meili keys server-side |
| Icons/UI | Minimal hand-rolled + a lean set (e.g. Lucide) | Keep chrome minimal, few elements |
| Testing | **Vitest** (unit), **Playwright** (e2e) | Standard Svelte testing stack |

> Diagrams (Mermaid/PlantUML/UML/GraphViz) are **not** rendered in the browser — they are
> produced server-side by Kroki at publish time and inlined as sanitized SVG. This removes
> the biggest client-side rendering/XSS/perf cost from the read path.

## 3. Platform services (containers)

| Service | Project | License | Role | Notes |
|---|---|---|---|---|
| Search | **Meilisearch** | MIT (engine) | Instant, typo-tolerant, faceted search | Core focus; see [05](05-search-and-indexing.md) |
| Database | **PostgreSQL** (16+) | PostgreSQL | Authoritative store | Well-understood, hardened, extensions available |
| Object storage | **Garage** | AGPL-3.0 | S3-compatible blob store (attachments) | Lightweight, self-host-first; chosen over MinIO (2025 community-edition cuts) |
| Diagrams | **Kroki** | MIT | Server-side render of Mermaid, PlantUML, GraphViz, C4, BPMN, etc. | One endpoint for many diagram languages |
| Rate-limit store | **Valkey** | BSD | Shared login/signup rate-limit counters | Redis-protocol compatible |

Identity is built in (email + argon2id password hash in Postgres); there is no external
IdP. TLS termination is left to whatever reverse proxy the operator already runs — none is
bundled.

## 4. How each Markdown requirement is met

| Requirement | Mechanism |
|---|---|
| Code blocks | goldmark fenced code + Shiki/highlight.js at publish |
| Mermaid | Kroki (`mermaid`) → SVG at publish |
| UML | Kroki (`plantuml`) → SVG at publish |
| Chemical schemas — equations | KaTeX + `mhchem` (`\ce{...}`) client-side |
| Chemical schemas — molecular structures | SMILES fenced block → SmilesDrawer client-side |
| Mathematical formulas | KaTeX (`$...$`, `$$...$$`) client-side |
| Diagrams (bonus: C4, BPMN, GraphViz, ERD) | Kroki, same pipeline |

**Custom fenced-block convention** (examples):
- ` ```mermaid ` → Kroki mermaid
- ` ```plantuml ` / ` ```uml ` → Kroki plantuml
- ` ```smiles ` → SmilesDrawer
- ` ```math ` or `$$…$$` → KaTeX
- `\ce{...}` inside math → mhchem

## 5. Dev tooling & CI

| Concern | Tool |
|---|---|
| Go lint | `golangci-lint` (govet, staticcheck, gosec) |
| Go vuln scan | `govulncheck` |
| JS/TS lint | ESLint + `svelte-check` + Prettier |
| Container scan | Trivy (image + IaC) |
| SAST | Semgrep |
| DAST (staging) | OWASP ZAP baseline |
| Dependency updates | Renovate/Dependabot |
| Load testing | k6 |
| SBOM | Syft (CycloneDX) |
| OpenAPI spec | `swaggo/swag` (annotations) or `ogen` (spec-first) | Public API contract → docs + SDKs ([10](10-public-api.md)) |
| API docs UI | Scalar or Redoc (self-hosted, at `/api/docs`) |
| API SDKs | `openapi-generator` (Go/TS/Python clients in CI) |
| CI | GitHub Actions (build, test, scan, publish signed images w/ cosign) |

## 6. Logging

No telemetry SDK or backend ships with LookupKB — the stack stays light. Operations rely
on structured logs plus `/healthz`/`/readyz`.

- **One logger, one format.** The Go service configures a single `slog` handler (JSON by
  default, text for dev) at startup — no ad-hoc `fmt.Println`, no per-package loggers.
  Standard fields on every line: `time`, `level`, `msg`, `service`, `version`,
  `request_id`, `actor_id` (internal user id, never the email).
- **Correlation.** `request_id` is set once per request by middleware and appears on every
  line logged under that request.
- **Levels & redaction.** Configurable log level per env; secrets/tokens/passwords never
  logged; request/response bodies not logged.

## 7. Licensing note

Target an OSI-approved license for LookupKB itself (candidate: **AGPL-3.0** to keep
network-deployed derivatives open, or **Apache-2.0** for maximum adoption — decide in
[09](09-open-questions.md)). Note bundled components' licenses (e.g. Garage's AGPL) for compatibility of the overall distribution; LookupKB integrates them over the
network (no static linking), so their licenses don't force LookupKB's license.
