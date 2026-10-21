-- Document queries: drafts, publish/unpublish and the read path. Access-policy
-- queries live in access.sql; keep
-- queries close to the feature that needs them (03-data-model.md, ticket note).

-- name: CreateDraft :one
-- Insert a new draft owned by the author. slug is caller-supplied (title + short
-- id, stable across edits); status defaults to 'draft'.
INSERT INTO documents (slug, author_id, title, description, source_md)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: SaveDraft :one
-- Overwrite the working copy in place (saves are not versioned). Scoped to
-- author_id so an author can only save their own document. Any status: editing a
-- published entry changes only the working copy — rendered_html and the search
-- index change on the next (re-)publish.
UPDATE documents
SET title       = $3,
    description = $4,
    source_md   = $5,
    updated_at  = now()
WHERE id = $1
  AND author_id = $2
  AND deleted_at IS NULL
RETURNING *;

-- name: SetRenderedHTML :exec
-- Cache the publish-time sanitized HTML into the documents.rendered_html column.
-- This is the ONLY write path to documents.rendered_html, and the value MUST be
-- render.RenderAndSanitize output — no unsanitized HTML ever reaches this column
-- (06-security.md §2; LOO-16). Status / published_at transitions are the
-- publish gate's job (LOO-14); this primitive only caches the HTML.
UPDATE documents
SET rendered_html = $2,
    updated_at    = now()
WHERE id = $1;

-- name: GetDraft :one
-- Read one of the author's own documents for the editor, in ANY status (draft/
-- published/unpublished — there is no status filter despite the name; editing a
-- published entry loads its working copy). Owner-scoped so a cross-author or
-- missing id yields pgx.ErrNoRows → 404 (03-data-model.md §2). deleted_at IS NULL
-- excludes soft-deleted rows.
SELECT *
FROM documents
WHERE id = $1
  AND author_id = $2
  AND deleted_at IS NULL;

-- name: GetOwnedDocument :one
-- Read one of the caller's own documents in ANY status (draft/published/
-- unpublished) — the publish gate (LOO-14) needs the working copy of an
-- already-published doc for a re-publish. (Same filter as GetDraft today; kept as
-- its own query so the editor read and the publish read can diverge.) Owner-scoped so a cross-author or missing id yields pgx.ErrNoRows
-- → an identical 404 that never leaks existence (03-data-model.md §2).
SELECT *
FROM documents
WHERE id = $1
  AND author_id = $2
  AND deleted_at IS NULL;

-- name: GetPublishedBySlug :one
-- The hot read path (F8, 01-architecture.md §3.2): fetch a PUBLISHED document by
-- its slug for the rendered-HTML view. status='published' means a draft/unpublished
-- (or soft-deleted) slug yields pgx.ErrNoRows → an identical 404, never partial
-- content. Reads the cached rendered_html column — no render on this path.
--   * JOIN users → author display name for the byline.
--   * tags aggregated as labels for the metadata display.
-- author_id and tag_slugs are selected only to feed the server-side access check
-- (access.Policy.CanRead: protected tags, LOO-33); they are NOT projected to the
-- client. published_md (the Markdown that produced rendered_html, LOO-38) feeds
-- the public API's ?format=markdown — never source_md, the working copy. One round
-- trip keeps the path fast.
SELECT
    d.id,
    d.slug,
    d.title,
    d.description,
    d.rendered_html,
    d.published_md,
    d.author_id,
    d.published_at,
    d.updated_at,
    COALESCE(u.display_name, split_part(u.email::text, '@', 1))::text AS author_display_name,
    COALESCE(array_remove(array_agg(t.label ORDER BY t.label), NULL), '{}')::text[] AS tags,
    COALESCE(array_remove(array_agg(t.slug ORDER BY t.slug), NULL), '{}')::text[] AS tag_slugs
FROM documents d
JOIN users u ON u.id = d.author_id
LEFT JOIN document_tags dt ON dt.document_id = d.id
LEFT JOIN tags t ON t.id = dt.tag_id
WHERE d.slug = $1
  AND d.status = 'published'
  AND d.deleted_at IS NULL
GROUP BY d.id, u.display_name, u.email;

-- name: PublishDocument :one
-- Flip a document to 'published' with its just-validated metadata and freshly
-- rendered HTML, inside the publish transaction (LOO-14). First-publish semantics
-- are encoded in SQL so they cannot race a concurrent read-modify-write:
--   * published_at = COALESCE(published_at, now()) — set once, never overwritten
--     on re-publish (breaks "first published" sort order otherwise).
--   * slug = CASE WHEN published_at IS NULL THEN @slug ELSE slug END — the ugly
--     draft-<hex> placeholder is replaced with the title-derived slug on the FIRST
--     publish only, then stays stable across edits (03-data-model.md §1 `slug`).
--   * published_md is the exact source rendered_html was rendered from (LOO-38),
--     so the published Markdown never drifts with later working-copy saves.
-- Owner-scoped (author_id) so it can never publish another author's document.
UPDATE documents
SET status        = 'published',
    title         = @title,
    description   = @description,
    rendered_html = @rendered_html,
    published_md  = @published_md,
    slug          = CASE WHEN published_at IS NULL THEN @slug ELSE slug END,
    published_at  = COALESCE(published_at, now()),
    updated_at    = now()
WHERE id = @id
  AND author_id = @author_id
  AND deleted_at IS NULL
RETURNING *;

-- name: UnpublishDocument :one
-- Revert a published document to author-only 'unpublished' visibility (AC-5.6).
-- Only a currently-published doc can be unpublished (status='published' guard), so
-- a draft/already-unpublished/other-author id yields pgx.ErrNoRows → 404.
-- published_at and rendered_html are left intact — unpublish removes the doc from
-- the index-eligible state, it does not erase its content. published_md is cleared
-- (LOO-38): only a published entry has a published source; the next publish
-- writes it afresh.
UPDATE documents
SET status       = 'unpublished',
    published_md = NULL,
    updated_at   = now()
WHERE id = $1
  AND author_id = $2
  AND status = 'published'
  AND deleted_at IS NULL
RETURNING *;

-- name: DeleteDraft :one
-- Hard-delete one of the author's own never-public documents (LOO-58): only a
-- 'draft' or 'unpublished' row may go — a published doc must be unpublished first,
-- so it yields pgx.ErrNoRows → the identical 404, like a missing or other-author
-- id. document_tags/document_acl cascade; index_outbox rows survive (no FK since
-- 00008). RETURNING status lets the caller enqueue a defensive index delete for a
-- previously-published (unpublished) doc.
DELETE FROM documents
WHERE id = $1
  AND author_id = $2
  AND status IN ('draft', 'unpublished')
  AND deleted_at IS NULL
RETURNING id, status;

-- name: ListDrafts :many
-- List the author's own drafts for their private workspace (AC-2.5). Owner-scoped
-- so no other user's drafts are ever returned (AC-2.3); status='draft' excludes
-- published/unpublished docs and deleted_at IS NULL excludes soft-deleted rows.
-- Ordered by recency (most recently edited first). Only the columns the workspace
-- list needs are projected — never source_md/rendered_html.
SELECT id, slug, status, title, updated_at
FROM documents
WHERE author_id = $1
  AND status = 'draft'
  AND deleted_at IS NULL
ORDER BY updated_at DESC;

-- name: ListMyDocuments :many
-- The caller's own entries for the history panel: drafts pinned first, then
-- everything else by most recent edit. Owner-scoped; never source/HTML.
SELECT id, slug, status, title, updated_at
FROM documents
WHERE author_id = $1
  AND deleted_at IS NULL
ORDER BY (status = 'draft') DESC, updated_at DESC
LIMIT 100;

-- name: ListDocumentTagLabels :many
-- Tag labels currently attached to a document (prefills the re-publish dialog).
SELECT t.label
FROM document_tags dt
JOIN tags t ON t.id = dt.tag_id
WHERE dt.document_id = $1
ORDER BY t.label;

-- name: GetIndexProjection :one
-- Everything the search index stores for one PUBLISHED document. Built from the
-- published columns (rendered_html, not the working-copy source_md). No access
-- attributes: read access is derived from `tags` + the group tag policy at query
-- time (LOO-33), so a policy change never needs a reindex.
SELECT
    d.id,
    d.slug,
    d.title,
    d.description,
    d.rendered_html,
    d.author_id,
    d.created_at,
    d.published_at,
    -- Display name, else the email's local part: full emails are never indexed.
    COALESCE(u.display_name, split_part(u.email::text, '@', 1))::text AS author,
    COALESCE(array_remove(array_agg(t.label ORDER BY t.label), NULL), '{}')::text[] AS tags
FROM documents d
JOIN users u ON u.id = d.author_id
LEFT JOIN document_tags dt ON dt.document_id = d.id
LEFT JOIN tags t ON t.id = dt.tag_id
WHERE d.id = $1
  AND d.status = 'published'
  AND d.deleted_at IS NULL
GROUP BY d.id, u.display_name, u.email;

-- name: ListPublishedDocumentIDs :many
-- Every published document id (search index bootstrap / rebuild).
SELECT id
FROM documents
WHERE status = 'published'
  AND deleted_at IS NULL;

-- name: ListPublishedDocuments :many
-- One page of the public catalogue (GET /api/v1/documents, MCP list_documents;
-- LOO-38): PUBLISHED documents the caller may read, newest first, metadata only
-- (never source_md / published_md / rendered_html).
--
-- The protected-tag rule (access.Policy, LOO-33) is applied HERE, before the
-- LIMIT, so a page is never short and never reveals a hidden entry: a row is kept
-- when the caller authored it or it carries none of the caller's denied tags
-- (denied_ids = access.Policy.DeniedIDs(); empty for an unrestricted caller).
--
-- Keyset pagination on (published_at, id) DESC: the cursor is the last row of the
-- previous page; both NULL on the first page. published_at is set once at first
-- publish and never moves, so the order is stable across re-publishes.
SELECT
    d.id,
    d.slug,
    d.title,
    d.description,
    d.published_at,
    d.updated_at,
    COALESCE(u.display_name, split_part(u.email::text, '@', 1))::text AS author_display_name,
    COALESCE(
        (SELECT array_agg(t.label ORDER BY t.label)
         FROM document_tags dt
         JOIN tags t ON t.id = dt.tag_id
         WHERE dt.document_id = d.id),
        '{}')::text[] AS tags
FROM documents d
JOIN users u ON u.id = d.author_id
WHERE d.status = 'published'
  AND d.deleted_at IS NULL
  AND (
      d.author_id = @caller_id
      OR NOT EXISTS (
          SELECT 1
          FROM document_tags dt
          WHERE dt.document_id = d.id
            AND dt.tag_id = ANY (@denied_ids::uuid[])
      )
  )
  AND (
      sqlc.narg('cursor_published_at')::timestamptz IS NULL
      OR (d.published_at, d.id) < (sqlc.narg('cursor_published_at')::timestamptz, sqlc.narg('cursor_id')::uuid)
  )
ORDER BY d.published_at DESC, d.id DESC
LIMIT @max_rows;
