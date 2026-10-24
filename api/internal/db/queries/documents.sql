-- Document queries: drafts, publish/unpublish and the read path. Access-policy
-- queries live in access.sql.

-- name: CreateDraft :one
-- slug is caller-supplied; status defaults to 'draft'.
INSERT INTO documents (slug, author_id, title, description, source_md)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: SaveDraft :one
-- Overwrite the working copy in place. Owner-scoped; any status: editing a
-- published entry changes only the working copy, rendered_html and the search
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

-- name: GetOwnedDocument :one
-- One of the author's own documents in ANY status, for the editor and the publish
-- gate. Owner-scoped: a cross-author or missing id yields pgx.ErrNoRows, so both
-- map to an identical 404.
SELECT *
FROM documents
WHERE id = $1
  AND author_id = $2
  AND deleted_at IS NULL;

-- name: GetPublishedBySlug :one
-- The hot read path: a PUBLISHED document by slug. A draft/unpublished/soft-deleted
-- slug yields pgx.ErrNoRows -> an identical 404. Reads the cached rendered_html;
-- no render here. author_id and tag_slugs feed the server-side access check
-- (access.Policy.CanRead) and are not projected to the client. published_md (the
-- source rendered_html came from) feeds ?format=markdown, never source_md.
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
-- Flip a document to 'published' inside the publish transaction. First-publish
-- semantics live in SQL so they cannot race a read-modify-write:
--   * published_at is set once and never overwritten on re-publish.
--   * slug is replaced by the title-derived one on the first publish only.
--   * published_md is the exact source rendered_html came from, so it never
--     drifts with later working-copy saves.
-- Owner-scoped.
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
-- Only a currently-published doc can be unpublished; anything else yields
-- pgx.ErrNoRows. published_at and rendered_html are kept; published_md is cleared
-- (only a published entry has one).
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
-- Hard-delete one of the author's own draft/unpublished documents; a published
-- doc yields pgx.ErrNoRows (identical 404). document_tags cascade; index_outbox
-- rows survive (no FK). RETURNING status lets the caller enqueue an index delete
-- for a previously-published doc.
DELETE FROM documents
WHERE id = $1
  AND author_id = $2
  AND status IN ('draft', 'unpublished')
  AND deleted_at IS NULL
RETURNING id, status;

-- name: ListDrafts :many
-- The author's own drafts, most recently edited first. Never projects
-- source_md/rendered_html.
SELECT id, slug, status, title, updated_at
FROM documents
WHERE author_id = $1
  AND status = 'draft'
  AND deleted_at IS NULL
ORDER BY updated_at DESC;

-- name: ListMyDocuments :many
-- History panel: drafts pinned first, then by most recent edit. Never source/HTML.
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

-- name: GetIndexProjections :many
-- Index documents for the given ids; only currently published documents are
-- returned. Built from rendered_html, never the working-copy source_md.
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
WHERE d.id = ANY(@ids::uuid[])
  AND d.status = 'published'
  AND d.deleted_at IS NULL
GROUP BY d.id, u.display_name, u.email;

-- name: ListPublishedDocuments :many
-- One page of the public catalogue (REST list, MCP list_documents): PUBLISHED
-- documents the caller may read, newest first, metadata only.
--
-- The protected-tag rule is applied here, before the LIMIT, so a page is never
-- short and never reveals a hidden entry: a row is kept when the caller authored
-- it or it carries none of the caller's denied tags (access.Policy.DeniedIDs()).
--
-- Keyset pagination on (published_at, id) DESC; both cursor values are NULL on the
-- first page. published_at never moves after first publish, so the order is stable.
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
