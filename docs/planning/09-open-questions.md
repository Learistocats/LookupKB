# 09 — Open Questions & Decisions to Confirm

Decisions already locked live in [README](README.md). Below are the choices still open,
grouped by urgency. Each has a recommendation to make resolution fast.

## A. Should be decided before/early in implementation

### Q1 — Tag vocabulary: free-form vs controlled?
Should authors invent tags freely, or pick from an admin-curated taxonomy (or hybrid: free
with admin merge/alias)?
- **Recommendation:** Hybrid — free-form entry with autocomplete + admin ability to merge/alias/deprecate tags. Prevents fragmentation without gatekeeping authoring.

### Q2 — Project license: AGPL-3.0 vs Apache-2.0?
Network-copyleft (keeps SaaS forks open) vs maximum permissive adoption.
- **Recommendation:** AGPL-3.0 for the server (aligns with "completely OSS", discourages closed SaaS forks), permissive for any client SDKs/libraries. Decide based on adoption goals.

### Q3 — Admin visibility into others' drafts?
Are private drafts truly private from admins, or can admins view them (governance)?
- **Recommendation:** Private by default (respects "private workspace"); provide an auditable "break-glass" admin access that logs to the audit trail, off by default.

### Q4 — Do we ship the PostgreSQL FTS degraded-mode fallback?
A basic keyword search if Meilisearch is down.
- **Recommendation:** Defer to post-v1; rely on Meilisearch HA + fast rebuild. Add `search_tsv` column now (cheap) so the option stays open.

### Q5 — Attachment type allowlist & size caps?
Exact MIME allowlist (images only? PDFs? office docs?) and max size.
- **Recommendation:** v1 = images (`png/jpeg/webp/gif/svg-sanitized`) + PDF, 25 MB cap; expand later. AV scan (ClamAV) hook optional.

### Q6 — Slug strategy & collisions?
Human-readable slug from title + short suffix vs opaque IDs.
- **Recommendation:** `kebab-title` + short base32 suffix (stable, collision-safe, readable). Title edits don't change slug.

## B. Can be decided during the relevant milestone

### Q7 — Redis/Valkey for LookupKB caching?
Valkey is already in the stack (login/signup rate-limit counters); should LookupKB also use it for read caching, or is in-process + Postgres enough?
- **Recommendation:** Start with in-process cache + Postgres `rendered_html`; adopt Valkey only if perf budgets require it. (Valkey over Redis for licensing.)

### Q8 — Syntax highlighting at publish (Shiki) vs client (highlight.js)?
Shiki gives editor-grade themes but is heavier at build/publish.
- **Recommendation:** Shiki at publish time (server-side, cached in rendered HTML) → zero client cost, best fidelity. Fall back to highlight.js if publish latency suffers.

### Q9 — Notifications?
Email/webhook on publish, on being granted group access, etc.
- **Recommendation:** Out of scope for v1; add a webhook/event stream post-v1.

### Q10 — Client-side diagram preview cost while editing?
Editor previews Mermaid/PlantUML by calling Kroki live — acceptable latency/coupling, or render on demand only (button)?
- **Recommendation:** Debounced on-demand preview (render on pause or explicit "preview" toggle) to avoid hammering Kroki while typing.

## C. Strategic / later

### Q11 — Multi-tenancy?
Single-org self-host (v1) vs multi-org isolation.
- **Recommendation:** Single-org v1; keep `access`/ACL model tenant-extensible. Revisit if SaaS demand appears.

### Q12 — Semantic/vector search?
Meilisearch supports vector/hybrid search; worth adding "related documents" & semantic recall?
- **Recommendation:** Post-v1 enhancement; keyword+typo search covers the core need first.

### Q13 — Real-time collaboration later?
Confirmed out of scope for v1 (single-author). Keep the data model Yjs-friendly?
- **Recommendation:** Don't pre-build, but avoid decisions that block a future Yjs document store.

### Q14 — Import/migration tooling?
Bulk import from Obsidian vaults / Confluence / Notion to seed a KB.
- **Recommendation:** Post-v1; a bulk-Markdown importer (folder of `.md` → drafts) is a cheap early win if adoption needs it.

### Q15 — API write operations?
v1 API is read-only ([10](10-public-api.md)). When (if) do we add create/update/publish via API, and does publish-via-API keep an equivalent confirm/guard?
- **Recommendation:** Post-v1. If added, gate programmatic publish behind an explicit scope + audit, and consider requiring `draft` ingest + separate publish to preserve the deliberate-publish principle.

### Q16 — GraphQL and/or webhooks?
Beyond REST, do consumers need GraphQL field-selection or outbound webhooks (publish/ACL-change events) to keep downstream systems (e.g. a RAG store) in sync?
- **Recommendation:** Ship REST+OpenAPI in v1. Add outbound webhooks first (high value for RAG/sync) then GraphQL only if consumer demand appears.

### Q17 — API rate-limit tiers & quotas?
Default per-token limits, and whether admins can set per-token/per-account tiers.
- **Recommendation:** Sensible global default (e.g. 60 req/min/token, burst 120) in v1; admin-configurable per-token tiers post-v1.

---

## Immediate asks for the project owner
The ones most worth answering now, before coding starts:
1. **Q1** tag vocabulary model.
2. **Q2** license.
3. **Q3** admin-vs-draft-privacy stance.
4. **Q5** attachment allowlist/limits.

Everything else can ride its milestone.
