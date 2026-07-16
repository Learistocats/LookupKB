# 02 — Technology Stack & Libraries

Guiding rule: **reuse proven OSS; write as little bespoke code as possible.** Every choice
below is open-source, self-hostable, and actively maintained (verify maintenance at
implementation time — see the "Maintenance watch" column).

## 1. Backend (Go)

| Concern | Library | Why | Maintenance watch |
|---|---|---|---|
| Web framework | `gofiber/fiber` (v2) | Express-like DX, very fast (built on `fasthttp`), batteries-included middleware, good OTel support | Active, large community |
| DB driver | `jackc/pgx` (v5) | Fastest, most complete Postgres driver | Active |
| Type-safe SQL | `sqlc` | Generates Go from SQL — no ORM runtime, compile-time safety | Active |
| Migrations | `pressly/goose` **or** `golang-migrate` | Versioned schema migrations | Active |
| OIDC | `coreos/go-oidc` + `golang.org/x/oauth2` | Standard OIDC/JWT verification against Authentik JWKS | Active |
| Markdown | `yuin/goldmark` (+GFM, footnotes, attributes) | CommonMark-compliant, extensible, safe, fast | Active, de-facto Go standard |
| HTML sanitizer | `microcosm-cc/bluemonday` | Allowlist HTML sanitization (XSS defense) | Active |
| Search client | `meilisearch/meilisearch-go` | Official Meilisearch SDK | Active |
| S3 client | `aws/aws-sdk-go-v2` (S3) | Works against Garage's S3 API | Active |
| Validation | `go-playground/validator` | Struct/field validation for metadata gates | Active |
| Config | `caarlos0/env` (or `spf13/viper`) | 12-factor env config | Active |
| Logging | `log/slog` (stdlib) + `otelslog` bridge | Structured logging, trace-correlated, no heavy dep | Stdlib + OTel |
| Telemetry | OpenTelemetry Go SDK + `otelfiber` + `otelpgx` | Traces, metrics, logs via OTLP; cloud-native, vendor-neutral | Active (see §6) |
| Rate limiting | `gofiber/fiber` limiter middleware | Abuse protection | Active |
| Testing | `stretchr/testify`, `testcontainers-go` | Assertions + real Postgres/Meili in integration tests | Active |

**Framework decision:** **Fiber (v2)** for its Express-like ergonomics, built-in
middleware (limiter, CORS, request-id, recover, healthcheck), and speed. Caveat: Fiber is
built on `fasthttp`, not `net/http`, so it is **not** wire-compatible with the `net/http`
middleware ecosystem. This is acceptable here — our sensitive integrations don't need it:
`coreos/go-oidc` verifies token *strings* (no `http.Request` dependency), and
`meilisearch-go` / `aws-sdk-go-v2` / `pgx` are outbound clients. Fiber has first-class OTel
via `otelfiber`. Behind Caddy (which terminates TLS/HTTP2 and speaks HTTP/1.1 upstream),
fasthttp's lack of native HTTP/2 is a non-issue. **Fallback:** `labstack/echo` (net/http)
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
| Identity | **Authentik** | MIT | Users, groups, OIDC, MFA | Reused user-management system; LookupKB never stores passwords |
| Search | **Meilisearch** | MIT (engine) | Instant, typo-tolerant, faceted search | Core focus; see [05](05-search-and-indexing.md) |
| Database | **PostgreSQL** (16+) | PostgreSQL | Authoritative store | Well-understood, hardened, extensions available |
| Object storage | **Garage** | AGPL-3.0 | S3-compatible blob store (attachments) | Lightweight, self-host-first; chosen over MinIO (2025 community-edition cuts) |
| Diagrams | **Kroki** | MIT | Server-side render of Mermaid, PlantUML, GraphViz, C4, BPMN, etc. | One endpoint for many diagram languages |
| Reverse proxy | **Caddy** | Apache-2.0 | TLS, security headers, static serving, edge rate-limit | Automatic HTTPS |
| Cache (optional) | **Redis/Valkey** | BSD | Authentik dependency; optional LookupKB read cache | Valkey if avoiding Redis license concerns |

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

## 6. Observability & logging (OpenTelemetry-first)

Everything the [test book](07-test-book.md) measures — search p95, read p95, index
freshness — is emitted as **OpenTelemetry** signals so acceptance criteria are verified
against real telemetry, not guesses. OTel is vendor-neutral: the app emits **OTLP** and the
operator points it at any backend.

| Signal | How | Notes |
|---|---|---|
| **Traces** | OTel Go SDK; `otelfiber` (HTTP), `otelpgx` (DB), manual spans around publish, render, Kroki, Meilisearch, Garage | End-to-end trace per request; each hot-path span timed |
| **Metrics** | OTel Metrics SDK | RED metrics per route + custom: `search_latency`, `read_latency`, `publish_total`, `index_lag_seconds`, `render_duration`, `kroki_duration` — the exact ACs from [04](04-functional-spec.md)/[07](07-test-book.md) |
| **Logs** | `log/slog` (JSON) bridged to OTel via `otelslog` | Trace-correlated (auto `trace_id`/`span_id`), single logger config, see below |
| **Export** | **OTLP → OpenTelemetry Collector** | One collector container fans out to the operator's choice of backend |

**Coherent, unified logging for the whole image/stack:**
- **One logger, one format.** The Go service configures a single `slog` JSON handler at
  startup and passes it via context everywhere — no ad-hoc `fmt.Println`, no per-package
  loggers with divergent formats. Standard fields on every line: `time`, `level`, `msg`,
  `service`, `version`, `trace_id`, `span_id`, `request_id`, `actor_id` (redacted PII).
- **Correlation.** Because logs carry `trace_id`, any log line jumps to its trace and its
  metrics — a coherent story across all three signals.
- **Whole-deployment coherence.** Third-party containers (Postgres, Meilisearch, Kroki,
  Garage, Authentik, Caddy) are configured to emit **structured/JSON logs where
  supported**, and the **OTel Collector normalizes** everything into one pipeline and one
  schema. Result: the deployed image emits one consistent, queryable log stream regardless
  of which component produced a line.
- **Levels & redaction.** Configurable log level per env; secrets/tokens never logged;
  request/response bodies not logged by default.

**Optional bundled observability stack** (Compose override, opt-in): OTel Collector +
Prometheus (metrics) + Tempo/Jaeger (traces) + Loki (logs) + Grafana (dashboards preloaded
with the AC budgets). Ships as `docker-compose.observability.yml` so operators can plug in
their own backend instead. Prometheus scrape of the collector remains available for
non-OTLP setups.

## 7. Licensing note

Target an OSI-approved license for LookupKB itself (candidate: **AGPL-3.0** to keep
network-deployed derivatives open, or **Apache-2.0** for maximum adoption — decide in
[09](09-open-questions.md)). Note bundled components' licenses (Garage & Authentik infra
choices) for compatibility of the overall distribution; LookupKB integrates them over the
network (no static linking), so their licenses don't force LookupKB's license.
