# 10 — Public API (Search / Query / Retrieve)

LookupKB exposes a **versioned, documented, read-only REST API** so external tools —
CLIs, dashboards, chatbots/RAG pipelines, CI jobs, other services — can search, query, and
retrieve documents programmatically. The API enforces the **same group-based access
control** as the UI: a caller only ever sees what its identity is allowed to see.

## 1. Principles

- **Read-only in v1.** Search, query/filter, list, retrieve (documents, tags). Write (ingest/publish) is designed-for but deferred — see [09](09-open-questions.md).
- **Same authz, always.** No endpoint bypasses the access model; search results and document reads are access-filtered server-side ([03](03-data-model.md) §2, [05](05-search-and-indexing.md) §3).
- **Stable & versioned.** Base path `/api/v1`; breaking changes go to `/api/v2`. Additive changes are non-breaking.
- **Machine-friendly.** JSON by default, OpenAPI 3 spec, cursor pagination, RFC 7807 errors, content negotiation, predictable rate limits.
- **Same surface as the app.** The SvelteKit UI consumes this same API (with its session token, via the web BFF); programmatic clients use tokens. One implementation, one authz path.

## 2. Authentication

One credential type, resolving to a user whose **group memberships drive access**:

### 2.1 Personal Access Tokens (PATs)
- Issued by a user in the LookupKB UI (Settings → API Tokens). The token **acts as that user** and inherits their current group memberships.
- Presented as `Authorization: Bearer lkb_pat_<...>`.
- Properties: name/label, scopes, optional expiry, last-used timestamp, revocable at any time.
- **Storage:** only a salted hash is stored (`api_tokens` table, [03](03-data-model.md)); the raw token is shown once at creation. A short non-secret prefix is stored for display/identification.
- Group resolution: at request time from `group_members`, so a PAT reflects the user's up-to-date access.

For system-to-system integrations, create a dedicated LookupKB user, add it to the groups
it needs, and issue it a PAT.

### 2.2 Scopes (read-only set for v1)
`search:read`, `documents:read`, `tags:read`. (Write scopes reserved for a future version.)
Tokens are least-privilege: a token missing a scope gets `403`.

> Whichever credential is used, **access decisions are identical** to the UI: `public` docs
> are visible to any authenticated identity; `restricted` docs require group intersection;
> drafts are never exposed via the public API.

## 3. Conventions

| Aspect | Convention |
|---|---|
| Base URL | `https://<host>/api/v1` |
| Format | `application/json` (default); see content negotiation §5 |
| Pagination | Cursor-based: `?limit=` (default 20, max 100) + `?cursor=`; response returns `next_cursor` |
| Sorting | `?sort=published_at:desc` (allowlisted fields) |
| Errors | RFC 7807 `application/problem+json` (`type`, `title`, `status`, `detail`, `instance`) |
| Rate limits | Per-token token-bucket; `X-RateLimit-Limit/Remaining/Reset` headers; `429` on exceed |
| Caching | `ETag` + `Last-Modified` on document reads; `If-None-Match` → `304` |
| Request IDs | `X-Request-Id` echoed; present in traces/logs ([01](01-architecture.md) §6) |
| Deprecation | `Deprecation` + `Sunset` headers when an endpoint/version is retiring |

## 4. Endpoints (v1)

### Search
```
GET /api/v1/search?q=<text>&tags=a,b&author=<id>&from=<ts>&to=<ts>&limit=&cursor=
```
Typo-tolerant, ranked full-text search across the caller's accessible corpus.
Returns hits with `title`, `description`, `tags`, `author`, `slug`, `published_at`,
`snippet` (highlighted), and `score`. Facets returned when `?facets=tags,author` is passed.
Access filter is injected server-side — restricted docs never appear.

### List / query documents
```
GET /api/v1/documents?tags=a,b&author=<id>&from=&to=&sort=published_at:desc&limit=&cursor=
```
Metadata-driven listing/filtering (no free-text ranking). Returns document metadata records
(no body by default). Use for enumeration/sync.

### Retrieve a document
```
GET /api/v1/documents/{slug}
GET /api/v1/documents/{slug}?format=markdown|html|text|json
```
Returns the published document: metadata + content in the requested format (see §5).
`403`/`404` (indistinguishable to avoid existence leaks) if the caller lacks access.

### Tags
```
GET /api/v1/tags?prefix=<p>&limit=          -> list tags (+usage counts) for discovery/autocomplete
```

### Meta
```
GET /api/v1/openapi.json   -> the OpenAPI 3 spec
GET /healthz  /readyz      -> operational (unversioned)
```

## 5. Content negotiation / formats

A retrieved document can be returned as (client chooses via `?format=` or `Accept`):

| Format | `?format=` | `Accept` | Use case |
|---|---|---|---|
| JSON envelope (default) | `json` | `application/json` | Metadata + embedded content field(s) |
| Raw Markdown | `markdown` | `text/markdown` | Portability, re-rendering, migration |
| Sanitized HTML | `html` | `text/html` | Display/embedding (pre-rendered, safe) |
| Plain text | `text` | `text/plain` | RAG / indexing / NLP (Markdown stripped) |

The JSON envelope includes `id, slug, title, description, tags, author, created_at,
published_at, access` plus the content in the requested representation(s).
`text` is the same `body_text` extraction used for search indexing ([05](05-search-and-indexing.md)).

## 6. Example

```bash
# Search (PAT)
curl -H "Authorization: Bearer $TOKEN" \
  "https://kb.example.com/api/v1/search?q=kubernetes+runbook&tags=ops&limit=10"

# Retrieve as plain text for a RAG pipeline
curl -H "Authorization: Bearer $TOKEN" -H "Accept: text/plain" \
  "https://kb.example.com/api/v1/documents/kubernetes-runbook-a1b2"
```

## 7. Security

- All §2 tokens verified before any handler runs; scope + group checks enforced per request.
- Search/retrieve reuse the server-side access filter — **no leakage via the API** (dedicated tests in [07](07-test-book.md) §4).
- Per-token rate limiting + global edge limits (DoS defense, [06](06-security.md)).
- PAT secrets stored hashed; shown once; revocable; optional expiry; `last_used_at` tracked for anomaly detection.
- The API never exposes drafts, other users' private data, Meilisearch keys, or ACL internals.
- Full treatment: [06 — Security](06-security.md) (API additions folded in).

## 8. Tooling & SDKs

- **OpenAPI 3** spec is the source of truth (`/api/v1/openapi.json`), generated from code annotations (`swaggo/swag` or `ogen`).
- Auto-generated interactive docs (Scalar/Redoc) served at `/api/docs`.
- Client SDKs (Go, TypeScript, Python) generated from the spec in CI — no hand-written clients to drift.

## 9. Out of scope for v1 (see [09](09-open-questions.md))
- Write operations (create/update/publish via API).
- GraphQL endpoint.
- Outbound webhooks / event stream on publish/ACL change.
- Bulk export endpoint (though `documents` listing + retrieve enables client-side export).
