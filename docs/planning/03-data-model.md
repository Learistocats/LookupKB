# 03 — Data Model & Access Control

Authoritative store: **PostgreSQL** — including identity. LookupKB owns its user accounts
(email + argon2id password hash) and its groups + membership; there is no external IdP.

## 1. Entities

### `users` (local accounts)
| Column | Type | Notes |
|---|---|---|
| `id` | uuid (PK) | LookupKB internal id; the access token's `sub` |
| `email` | citext (unique, not null) | Login identifier; case-insensitive |
| `password_hash` | text (not null) | argon2id PHC string (`$argon2id$v=19$m=…,t=…,p=…$salt$key`) |
| `display_name` | text null | Optional, set at signup |
| `created_at` | timestamptz | Signup time |
| `last_seen_at` | timestamptz | Updated on login |

> `password_hash` is the **only** persisted form of a password — never plaintext or
> reversibly encrypted, never logged, never returned by any endpoint. Signup is open
> (self-service).

### `groups`
| Column | Type | Notes |
|---|---|---|
| `id` | uuid (PK) | Internal id |
| `name` | text (unique) | Human-readable (e.g. `security`, `hr`) |
| `created_at` | timestamptz | |

### `group_members`
| Column | Type | Notes |
|---|---|---|
| `group_id` | uuid (FK groups, cascade) | |
| `user_id` | uuid (FK users, cascade) | |

Membership is authoritative here. The API loads the caller's group ids from this table
on **every** authenticated request (never baked into the token), so a membership change
takes effect immediately.

### `documents`
| Column | Type | Notes |
|---|---|---|
| `id` | uuid (PK) | |
| `slug` | text (unique) | URL-safe, derived from title + short id; stable across edits |
| `author_id` | uuid (FK users) | Original author / owner |
| `status` | enum(`draft`,`published`,`unpublished`) | Lifecycle state |
| `title` | text | Current title (nullable while draft) |
| `description` | text | Short description (nullable while draft) |
| `source_md` | text | Current Markdown source (working copy) |
| `rendered_html` | text | Sanitized HTML of the published content (cache; overwritten on re-publish) |
| `access` | enum(`public`,`restricted`) | Default `public` |
| `created_at` | timestamptz | |
| `updated_at` | timestamptz | |
| `published_at` | timestamptz null | First publish |
| `search_tsv` | tsvector (optional) | Postgres FTS fallback / reindex source |

> **No version history.** Draft saves overwrite `documents.source_md` in place; a
> (re-)publish overwrites `rendered_html` and the metadata. There is no snapshot table.

### `tags`
| Column | Type | Notes |
|---|---|---|
| `id` | uuid (PK) | |
| `slug` | text (unique) | Normalized (lowercase, kebab) |
| `label` | text | Display form |
| `usage_count` | int | For suggestions/facets |
| `created_at` | timestamptz | |

### `document_tags` (M:N)
| `document_id` | uuid (FK) |
| `tag_id` | uuid (FK) |
PK (`document_id`,`tag_id`).

### `document_acl` (restricted docs → allowed groups)
| Column | Type | Notes |
|---|---|---|
| `document_id` | uuid (FK) | |
| `group_id` | uuid (FK groups) | Allowed group |
PK (`document_id`,`group_id`). Empty for `public` documents.

### `attachments`
| Column | Type | Notes |
|---|---|---|
| `id` | uuid (PK) | |
| `document_id` | uuid null (FK) | Owning doc (null while drafting) |
| `owner_id` | uuid (FK users) | Uploader |
| `s3_key` | text | Content-addressed key in Garage |
| `content_type` | text | Validated MIME |
| `size_bytes` | bigint | |
| `sha256` | text | Dedupe / integrity |
| `created_at` | timestamptz | |

### `audit_log`
| Column | Type | Notes |
|---|---|---|
| `id` | bigserial (PK) | |
| `actor_id` | uuid null | User or system |
| `action` | text | e.g. `document.publish`, `document.unpublish`, `acl.change`, `group.sync` |
| `target_type` | text | `document`, `group`, … |
| `target_id` | text | |
| `metadata` | jsonb | Diff / context |
| `at` | timestamptz | |

### `settings` (admin-tunable limits)
Key/value (jsonb) for metadata limits: `title.min/max`, `description.min/max`,
`tags.min/max`, `tag.min/max`, tag-vocabulary mode (`free`/`controlled`).

### `api_tokens` (Personal Access Tokens for the public API)
| Column | Type | Notes |
|---|---|---|
| `id` | uuid (PK) | |
| `user_id` | uuid (FK users) | Token acts as this user; inherits their group access |
| `name` | text | User-chosen label |
| `token_hash` | text | Salted hash of the secret (raw token never stored) |
| `prefix` | text | Short non-secret prefix for display/identification |
| `scopes` | text[] | e.g. `search:read`, `documents:read`, `tags:read` |
| `created_at` | timestamptz | |
| `last_used_at` | timestamptz null | Anomaly detection / hygiene |
| `expires_at` | timestamptz null | Optional expiry |
| `revoked_at` | timestamptz null | Soft-revoke |

> Programmatic clients authenticate with a PAT bound to a LookupKB user; there are no
> separate service-account identities. See [10 — Public API](10-public-api.md) §2.

## 2. Access-control model

**Default: every published document is `public`** (readable by any authenticated user).
An Admin can create named groups and manage their members (`group_members`), and an
Author/Admin can mark a document
`restricted` to one or more groups.

**Decision rule for read access to a published doc:**
```
allowed = (doc.access == 'public')
          OR (doc.access == 'restricted'
              AND intersect(user.groups, doc.acl_group_ids) != ∅)   -- user.groups from group_members
          OR (user.id == doc.author_id)     -- authors always see their own
          OR (user.is_admin)                -- admins see all
```

- **Multiple groups per user**: native — `group_members` holds all of the user's groups; access is an intersection test.
- **Multiple groups per document**: `document_acl` allows OR-semantics across groups.
- **Enforcement points** (defense in depth):
  1. API authorization middleware on every document read/write.
  2. Search: access filter injected into every Meilisearch query (docs carry ACL facets); users never receive hits they can't open.
  3. Attachments served through the API with the same check (no public buckets).

**Draft privacy:** drafts are visible only to `author_id` (and never indexed). Admins do
**not** see others' drafts by default (privacy of the private workspace) — configurable in
[09](09-open-questions.md).

## 3. Index projection (Meilisearch document)
See [05](05-search-and-indexing.md) for full schema. Each published document is projected to:
`{ id, slug, title, description, body_text, tags[], author, author_id, created_at,
published_at, access, acl_group_ids[] }` where `body_text` is a searchable plain-text
extraction (Markdown stripped) and `access`/`acl_group_ids` are filterable facets.

## 4. Consistency & indexing
- Postgres is source of truth; Meilisearch is updated transactionally-ish: on publish/
  unpublish/edit-republish/ACL-change, enqueue an index op (outbox pattern) so a failed
  Meili write can be retried without losing the DB commit.
- `POST /admin/reindex` rebuilds the entire Meili index from Postgres (DR + schema changes).

## 5. Retention
- Audit log retained ≥ 1 year (configurable).
- Deleted documents: soft-delete (status + `deleted_at`), purge job removes blobs & index entries after a grace period.
