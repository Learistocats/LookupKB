# 05 — Search & Indexing Design

Search is the **primary feature**. This document specifies the Meilisearch index, the
access-filtered query path, and the indexing pipeline that keeps the index consistent with
PostgreSQL.

## 1. Why Meilisearch

- Instant, **search-as-you-type** with sub-tens-of-ms latency on a single node.
- Built-in **typo tolerance**, prefix search, and relevancy ranking with sane defaults.
- **Faceted search & filtering** (tags, author, date, access) out of the box.
- Simple ops (one binary/container), MIT-licensed engine, scales to millions of docs.
- Managed via a clean HTTP API and an official Go SDK.

## 2. Index schema

**Index:** `documents`

```jsonc
{
  "id": "uuid",                 // primary key
  "slug": "kubernetes-runbook",
  "title": "Kubernetes Runbook",
  "description": "How we operate our clusters.",
  "body_text": "plain-text extraction of the Markdown (no syntax, no code fences noise)",
  "tags": ["kubernetes", "ops", "runbook"],
  "author": "Léa Guechoum",
  "author_id": "uuid",
  "created_at": 1731542400,     // unix ts (filterable/sortable)
  "published_at": 1731628800,
  "access": "public",           // or "restricted"   -> filterable facet
  "acl_group_ids": ["uuid", …]  // empty when public -> filterable facet
}
```

### Settings
- **Searchable attributes** (ranked order): `title` > `description` > `tags` > `body_text`.
- **Filterable attributes:** `tags`, `author_id`, `access`, `acl_group_ids`, `created_at`, `published_at`.
- **Sortable attributes:** `published_at`, `created_at`.
- **Displayed attributes:** everything except `body_text` (return a snippet instead).
- **Ranking rules:** default (words, typo, proximity, attribute, sort, exactness) + optional custom `published_at:desc` tiebreaker.
- **Typo tolerance:** enabled; min word size for 1 typo ≈ 4, for 2 typos ≈ 8 (tune).
- **Stop words / synonyms:** configurable per deployment (e.g. `k8s` ↔ `kubernetes`).
- **Highlighting / crop:** enabled to return matched snippets.

## 3. Access-filtered query path (security-critical)

The browser **never** talks to Meilisearch. Flow:

```
Browser ──POST /api/search {q, facets, page}──► LookupKB API
   API: derive access filter from caller's token groups
        filter = "access = 'public'
                  OR acl_group_ids IN [g1, g2, …]
                  OR author_id = <me>"
   API ──Meili search (scoped read key, injected filter)──► Meilisearch
   Meilisearch ──ranked hits──► API ──sanitized hits──► Browser
```

- The access filter is **always** appended server-side; a client cannot remove or widen it.
- Meilisearch **tenant tokens** (scoped, expiring, filter-embedded keys) are an additional
  option, but the primary guarantee is server-side filter injection. The public
  search/master keys are never shipped to the client.
- Admins get an unfiltered variant only via an explicitly authorized admin endpoint.

**Result shape returned to client:** `title`, `description`, `tags`, `author`, `slug`,
`published_at`, `snippet` (highlighted). No `body_text`, no ACL internals.

## 4. Indexing pipeline (consistency with Postgres)

PostgreSQL is the source of truth; Meilisearch is a derived, rebuildable projection.

### Triggers that mutate the index
| Event | Index action |
|---|---|
| Publish (first or re-publish) | Upsert document projection |
| Unpublish | Delete from index |
| ACL change on published doc | Upsert (updates `access`/`acl_group_ids`) |
| Metadata/tag edit + re-publish | Upsert |
| Delete document | Delete from index |
| Admin reindex | Bulk rebuild from Postgres |

### Reliability: transactional outbox
1. On a mutating action, write the domain change **and** an `index_outbox` row in the same Postgres transaction.
2. A background worker reads the outbox, applies the change to Meilisearch, marks it done, retries with backoff on failure.
3. This guarantees the index eventually matches Postgres even if Meilisearch is briefly down — no lost updates, no dual-write inconsistency.

### Body-text extraction
`body_text` is extracted from the **published** `rendered_html` (tags dropped, block
boundaries become spaces, `script`/`style`/`svg`/`math` skipped) when the worker builds the
projection — never from `source_md`, which may hold unpublished edits to a published entry.
Being derived from a stored column, it is deterministic for reindex.

### Worker (implemented in `api/internal/search`)
- Runs inside the API process when both Postgres and Meilisearch are configured.
- On start: create the index + apply settings (waits for the task), requeue ops left
  `processing` by a crash, and — if the index is empty — enqueue an upsert for every
  published document (bootstrap/recovery).
- Loop: claim up to 50 `pending` ops (`FOR UPDATE SKIP LOCKED`), apply, mark `done`; a
  failure increments `attempts` and returns the op to `pending` until 5 attempts, then
  `failed`. Polls every second (freshness target < 2 s) with backoff while Meilisearch is
  down. An `upsert` for a document that is no longer published deletes it instead.
- Query path: `GET /api/search?q=&limit=` (UI) — the access filter is built from the
  verified caller (UUIDs validated before they reach the filter); highlights use control
  characters as markers, then the snippet is HTML-escaped and only `<mark>` is re-allowed.
  An empty query returns the latest published entries.

### Full rebuild
`POST /admin/reindex` streams all published documents from Postgres, rebuilds settings +
documents in a fresh index, then atomically swaps (Meilisearch index swap) to avoid
downtime. Used for DR and schema/settings changes.

## 5. Performance budget & scaling

| Metric | Target |
|---|---|
| p50 search latency (100k docs) | < 20 ms (engine) / < 50 ms (end-to-end incl. API) |
| p95 search latency (100k docs) | < 100 ms end-to-end |
| Index freshness after publish | < 2 s (outbox worker) |
| Full reindex (100k docs) | minutes, online (index swap) |

Scaling notes: single-node Meilisearch comfortably handles millions of documents; RAM is
the main lever. Shard/cluster only if the corpus or QPS exceeds a single node — out of
scope for v1 but the outbox + rebuild design makes migration safe.

## 6. Fallback / degraded mode

If Meilisearch is unavailable, `/readyz` reports not-ready and the UI shows a degraded
banner. An optional PostgreSQL FTS fallback (`search_tsv` GIN index) can serve basic
keyword search so the KB remains partially usable — decide whether to ship this in
[09](09-open-questions.md).

## 7. Test hooks
See [07](07-test-book.md): typo-tolerance cases, access-filter leak tests (a user must
never receive a hit they cannot open), latency load tests (k6), and outbox
crash-consistency tests.
