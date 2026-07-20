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

-- name: GetDraft :one
-- Read a single draft owned by the author (drafts are private to their author,
-- 03-data-model.md §2). deleted_at IS NULL excludes soft-deleted rows.
SELECT *
FROM documents
WHERE id = $1
  AND author_id = $2
  AND deleted_at IS NULL;
