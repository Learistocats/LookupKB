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

-- name: GetOwnedDocument :one
-- Read one of the caller's own documents in ANY status (draft/published/
-- unpublished) — the publish gate (LOO-14) needs the working copy of an
-- already-published doc for a re-publish, which GetDraft (status='draft' only)
-- cannot return. Owner-scoped so a cross-author or missing id yields pgx.ErrNoRows
-- → an identical 404 that never leaks existence (03-data-model.md §2).
SELECT *
FROM documents
WHERE id = $1
  AND author_id = $2
  AND deleted_at IS NULL;

-- name: PublishDocument :one
-- Flip a document to 'published' with its just-validated metadata and freshly
-- rendered HTML, inside the publish transaction (LOO-14). First-publish semantics
-- are encoded in SQL so they cannot race a concurrent read-modify-write:
--   * published_at = COALESCE(published_at, now()) — set once, never overwritten
--     on re-publish (breaks "first published" sort order otherwise).
--   * slug = CASE WHEN published_at IS NULL THEN @slug ELSE slug END — the ugly
--     draft-<hex> placeholder is replaced with the title-derived slug on the FIRST
--     publish only, then stays stable across edits (03-data-model.md §1 `slug`).
-- Owner-scoped (author_id) so it can never publish another author's document.
UPDATE documents
SET status        = 'published',
    title         = @title,
    description   = @description,
    rendered_html = @rendered_html,
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
-- published_at, current_version_id and the document_versions history are all left
-- intact — unpublish removes the doc from the index-eligible state, it does not
-- erase its past.
UPDATE documents
SET status     = 'unpublished',
    updated_at = now()
WHERE id = $1
  AND author_id = $2
  AND status = 'published'
  AND deleted_at IS NULL
RETURNING *;

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
