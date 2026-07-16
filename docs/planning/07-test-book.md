# 07 — Test Book

Traceable test plan. Each entry links a functional/NFR acceptance criterion (from
[04](04-functional-spec.md)) to concrete test cases and the layer that verifies it.

## 1. Test layers & tooling

| Layer | Tooling | Scope |
|---|---|---|
| Unit | Go `testing`+`testify`; Vitest (front) | Pure logic: validation, ACL rule, body-text extraction, slugging |
| Integration | `testcontainers-go` (Postgres, Meilisearch, Kroki, Garage/S3 mock) | API ↔ real services: publish, index, search, uploads |
| Contract | OIDC mock / Authentik test realm | Token verification, group claims |
| E2E | Playwright | Browser flows: auth, draft, publish, search, read, restrict |
| Performance | k6 | Search & read latency under load |
| Security | Semgrep, gosec, govulncheck, Trivy, OWASP ZAP, XSS corpus | SAST/DAST/deps/sanitization/access-control |
| DR | Scripted | Reindex from Postgres, backup/restore |

## 2. Performance budgets (pass/fail gates)

Corpus: **100k published documents**, realistic sizes, warm cache. **All budgets are
measured from OpenTelemetry metrics/traces** (see [01](01-architecture.md) §6): k6 drives
load, and pass/fail is read from the OTel `search_latency`/`read_latency`/`index_lag_seconds`
instruments (Grafana dashboards preload these budgets), so acceptance is verified against
the same telemetry the running system emits in production.

| Metric | Budget | Test |
|---|---|---|
| Search p50 (end-to-end) | < 50 ms | PERF-1 (k6) |
| Search p95 (end-to-end) | < 100 ms | PERF-1 |
| Published read p95 (warm) | < 100 ms | PERF-2 |
| Published read p95 (cold) | < 300 ms | PERF-2 |
| Index freshness after publish | < 2 s | PERF-3 |
| Full reindex (100k) | completes online, no read downtime | PERF-4 |
| Cold `docker compose up` → usable | < 5 min | OPS-1 |

## 3. Functional test matrix

### Auth (F1)
- **T-1.1** (AC-1.1) Unauthenticated route access → redirect to Authentik → returns authenticated. *(E2E)*
- **T-1.2** (AC-1.2) Tampered/expired/invalid-signature JWT → 401. *(Integration)*
- **T-1.3** (AC-1.3) Expired access token → silent refresh success; refresh-fail → re-auth. *(E2E)*
- **T-1.4** (AC-1.4) Token groups drive access decision (parametrized groups). *(Integration)*
- **T-1.5** (AC-1.5) No password field/endpoint exists in LookupKB. *(Unit/Contract)*

### Drafts (F2)
- **T-2.1** (AC-2.1/2.2) Create + save draft with empty metadata → 200, no validation error. *(Integration)*
- **T-2.2** (AC-2.3) Draft not visible/searchable to other users (incl. admin per policy). *(Integration+E2E)*
- **T-2.3** (AC-2.4) Saved draft absent from Meilisearch index. *(Integration)*
- **T-2.4** (AC-2.5) Workspace lists only own drafts. *(E2E)*
- **T-2.5** (AC-2.6) Auto-save persists after debounce; dirty indicator shown. *(E2E)*

### Editor & rendering (F3)
- **T-3.1** (AC-3.1) Live preview renders headings/bold/lists/links. *(E2E/Vitest)*
- **T-3.2** (AC-3.2) Code block highlighted. *(E2E)*
- **T-3.3** (AC-3.3) Mermaid/PlantUML/UML preview renders; publish inlines sanitized SVG. *(Integration+E2E)*
- **T-3.4** (AC-3.4) KaTeX math + `\ce{}` chemistry render. *(E2E)*
- **T-3.5** (AC-3.5) SMILES block renders molecule. *(E2E)*

### Attachments (F4)
- **T-4.1** (AC-4.1) Paste image → uploaded to Garage, embed inserted. *(E2E+Integration)*
- **T-4.2** (AC-4.2) Disallowed type / oversized → rejected with message. *(Integration)*
- **T-4.3** (AC-4.3) Reader without doc access → attachment denied. *(Integration)* **[access-control]**

### Publish gate (F5)
- **T-5.1** (AC-5.1) Publish shows confirm step with metadata summary. *(E2E)*
- **T-5.2** (AC-5.2) Missing title/description/tag or limit violation → publish blocked, per-field errors, stays draft. *(Integration+E2E)*
- **T-5.3** (AC-5.3) Valid metadata → published, `published_at` set, snapshot created, indexed. *(Integration)*
- **T-5.4** (AC-5.5) Re-publish → new version snapshot; index updated. *(Integration)*
- **T-5.5** (AC-5.6) Unpublish → removed from index, author-only, history retained. *(Integration)*

### Tags (F6)
- **T-6.1** (AC-6.1) Autocomplete suggests existing tags with counts. *(E2E)*
- **T-6.2** (AC-6.2) New tag normalized to lowercase-kebab. *(Unit)*
- **T-6.3** (AC-6.3) <1 or >max tags → blocked. *(Integration)*

### Search (F7) — the focus
- **T-7.1** (AC-7.1/PERF-1) As-you-type results < 100 ms p95 @100k. *(Perf)*
- **T-7.2** (AC-7.2) Typo query returns correct doc. *(Integration)*
- **T-7.3** (AC-7.3) Hits show title/desc/tags/author/highlighted snippet. *(E2E)*
- **T-7.4** (AC-7.4) **Restricted doc never appears** for non-member (parametrized across many group combos). *(Integration)* **[access-control, critical]**
- **T-7.5** (AC-7.5) Facet filters (tags/author/date) narrow results. *(Integration)*
- **T-7.6** (AC-7.6) Meili host/key absent from client bundle & network calls. *(E2E+SAST)*
- **T-7.7** Facet counts exclude inaccessible docs (no count-based leakage). *(Integration)* **[access-control]**

### Read (F8)
- **T-8.1** (AC-8.1/PERF-2) Read latency budget met. *(Perf)*
- **T-8.2** (AC-8.2) Diagrams present as inlined SVG; no diagram JS loaded. *(E2E)*
- **T-8.3** (AC-8.4) Restricted doc by direct URL → 403/404, no content leak. *(Integration)* **[access-control]**

### Versioning (F9)
- **T-9.1** (AC-9.1) History lists versions with metadata. *(Integration)*
- **T-9.2** (AC-9.2) Diff between versions. *(Integration)*
- **T-9.3** (AC-9.3) Restore old version → new current snapshot. *(Integration)*

### Groups & admin (F10)
- **T-10.1** (AC-10.1) Authentik group sync → usable as ACL. *(Integration/Contract)*
- **T-10.2** (AC-10.2) Restrict doc to groups → only members + author + admin can read/find. *(Integration+E2E)* **[access-control]**
- **T-10.3** (AC-10.3) ACL change reflected in index & reads immediately; audited. *(Integration)*
- **T-10.4** (AC-10.4) Changing metadata limits enforced on next publish. *(Integration)*
- **T-10.5** (AC-10.5) Publish/unpublish/ACL change writes audit entry. *(Integration)*

### Ops/DR (F11)
- **T-11.1** (AC-11.1) `/healthz` & `/readyz` semantics (kill a dependency → not ready). *(Integration)*
- **T-11.2** (AC-11.2/PERF-4) Wipe Meili → reindex → search restored, online. *(DR)*
- **T-11.3** (AC-11.3/OPS-1) Clean-host compose up < 5 min usable. *(DR/manual+scripted)*

### Public API (F12)
- **T-12.1** (AC-12.1/12.2) PAT authenticates as issuing user with their groups; service-account token authenticates with claim groups. *(Integration)*
- **T-12.2** (AC-12.3) Invalid/expired/revoked token or missing scope → 401/403, no data. *(Integration)*
- **T-12.3** (AC-12.4) `/api/v1/search` never returns docs the caller can't access (parametrized group matrix). *(Integration)* **[access-control, critical]**
- **T-12.4** (AC-12.5) List/query with filters + cursor pagination (limit cap, `next_cursor`) correct. *(Integration)*
- **T-12.5** (AC-12.6) Retrieve returns correct content per `?format=`/`Accept` (markdown/html/text/json). *(Integration)*
- **T-12.6** (AC-12.7) Restricted doc via API → 403/404, no content/existence leak; drafts never retrievable via public API. *(Integration)* **[access-control, critical]**
- **T-12.7** (AC-12.8/12.9) Version endpoints & tags endpoint access-checked and correct. *(Integration)*
- **T-12.8** (AC-12.10) Per-token rate limit → 429 with headers. *(Integration)*
- **T-12.9** (AC-12.11) `openapi.json` valid & matches live routes (contract test); `/api/docs` served. *(Integration/CI)*
- **T-12.10** (AC-12.12) `ETag` + `If-None-Match` → 304. *(Integration)*
- **T-12.11** (AC-12.13) PAT lifecycle: create (secret shown once), scope, list-by-prefix, revoke; only hash persisted. *(Integration+E2E)*

## 4. Security test suite (see [06](06-security.md))

- **SEC-1 XSS corpus:** a battery of malicious Markdown/HTML/SVG payloads; assert none survive sanitization in rendered output. *(Integration)*
- **SEC-2 Access-control matrix:** exhaustive user×group×doc-access combinations across read, search, facets, attachments, version history, **and the public API (PAT + service account)** — **zero leakage**; the UI and API share one access path and are tested against the same matrix. *(Integration)*
- **SEC-3 IDOR:** enumerate draft/attachment/version IDs as non-owner → denied. *(Integration)*
- **SEC-4 SSRF:** diagram/embed remote-include attempts blocked; egress restricted. *(Integration)*
- **SEC-5 AuthN:** forged/expired/wrong-audience tokens rejected. *(Integration)*
- **SEC-6 Rate limiting/DoS:** oversized upload, huge diagram, query flood → bounded/rejected. *(Integration+Perf)*
- **SEC-7 Static analysis gates:** gosec, govulncheck, Semgrep, Trivy — no high/critical. *(CI)*
- **SEC-8 DAST:** OWASP ZAP baseline vs staging — no high alerts. *(CI/staging)*
- **SEC-9 Headers/CSP:** assert CSP + security headers present and effective. *(E2E)*

## 5. Definition of Done (per feature)
1. Acceptance criteria have passing automated tests at the right layer.
2. Security tests relevant to the feature pass (esp. access-control for anything touching visibility).
3. No new high/critical from SAST/deps/image scans.
4. Perf budgets met for hot-path features (search/read).
5. Docs updated (user + ops).
6. Audit logging present for state-changing admin/publish actions.

## 6. CI gates
PR must pass: build, unit, integration (testcontainers), lint, gosec, govulncheck, Semgrep,
Trivy, `svelte-check`. Perf & DAST run on merge-to-main/nightly against staging.
