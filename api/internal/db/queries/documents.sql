-- Walking-skeleton draft path only (M0): create, save, read a draft. Publish,
-- versioning, ACL and search queries arrive with their own milestones — keep
-- queries close to the feature that needs them (03-data-model.md, ticket note).

-- name: CreateDraft :one
-- Insert a new draft owned by the author. slug is caller-supplied (title + short
-- id, stable across edits); status defaults to 'draft'.
INSERT INTO documents (slug, author_id, title, description, source_md)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: SaveDraft :one
-- Overwrite the working copy in place (draft saves are not versioned). Scoped to
-- author_id so an author can only save their own draft.
UPDATE documents
SET title       = $3,
    description = $4,
    source_md   = $5,
    updated_at  = now()
WHERE id = $1
  AND author_id = $2
  AND status = 'draft'
RETURNING *;

-- name: SetRenderedHTML :exec
-- Cache the publish-time sanitized HTML into the current-version cache column.
-- This is the ONLY write path to documents.rendered_html, and the value MUST be
-- render.RenderAndSanitize output — no unsanitized HTML ever reaches this column
-- (06-security.md §2; LOO-16). Status / published_at / version-snapshot
-- transitions are the publish gate's job (LOO-14/LOO-17); this primitive only
-- caches the HTML so those tickets compose it into their publish transaction.
UPDATE documents
SET rendered_html = $2,
    updated_at    = now()
WHERE id = $1;

-- name: GetDraft :one
-- Read a single draft owned by the author (drafts are private to their author,
-- 03-data-model.md §2). deleted_at IS NULL excludes soft-deleted rows.
SELECT *
FROM documents
WHERE id = $1
  AND author_id = $2
  AND deleted_at IS NULL;

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
