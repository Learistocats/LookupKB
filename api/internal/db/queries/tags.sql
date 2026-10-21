-- Tag vocabulary queries (F6, 03-data-model.md `tags`/`document_tags`). Tag slugs
-- are normalized in Go (internal/tags.Normalize) before they reach any of these,
-- so the DB only ever sees canonical lowercase-kebab slugs. usage_count reflects
-- LIVE published usage and is maintained by the increment/decrement queries below,
-- driven by the transactional SetDocumentTags apply (internal/tags/store.go).

-- name: SearchTags :many
-- Autocomplete: match tags whose slug contains the (already-normalized) pattern,
-- most-used first (AC-6.1). The pattern is built in Go as '%slug%'; Normalize
-- strips LIKE metacharacters (% _), so there is no injection risk.
-- excluded_ids are the protected tags the caller lacks (access.Policy, LOO-33):
-- they are never suggested. is_protected lets the publish dialog tell the author
-- an entry carrying the tag will be restricted.
SELECT t.id,
       t.slug,
       t.label,
       t.usage_count,
       t.created_at,
       EXISTS (SELECT 1 FROM group_tag_grants g WHERE g.tag_id = t.id) AS is_protected
FROM tags t
WHERE t.slug LIKE @pattern
  AND NOT (t.id = ANY (@excluded_ids::uuid[]))
ORDER BY t.usage_count DESC, t.slug ASC
LIMIT @max_rows;

-- name: UpsertTag :one
-- Get-or-create a tag by its canonical slug. ON CONFLICT DO UPDATE (not DO
-- NOTHING) so the existing row is returned on conflict. usage_count is deliberately
-- NOT touched here — membership changes maintain it (see Increment/DecrementTagUsage).
INSERT INTO tags (slug, label)
VALUES ($1, $2)
ON CONFLICT (slug) DO UPDATE
SET label = EXCLUDED.label
RETURNING id, slug, label, usage_count, created_at;

-- name: ListDocumentTags :many
-- The tags currently attached to a document, used as the diff source when
-- re-applying a document's tag set (SetDocumentTags).
SELECT t.id, t.slug
FROM document_tags dt
JOIN tags t ON t.id = dt.tag_id
WHERE dt.document_id = $1;

-- name: AddDocumentTag :exec
-- Attach a tag to a document. ON CONFLICT DO NOTHING keeps the apply idempotent if
-- the pair already exists (the usage_count increment is issued separately, only
-- for tags the diff determined are genuinely new to the document).
INSERT INTO document_tags (document_id, tag_id)
VALUES ($1, $2)
ON CONFLICT (document_id, tag_id) DO NOTHING;

-- name: RemoveDocumentTag :exec
-- Detach a tag from a document (paired with DecrementTagUsage in the same tx).
DELETE FROM document_tags
WHERE document_id = $1
  AND tag_id = $2;

-- name: IncrementTagUsage :exec
-- Bump a tag's live published-usage counter by one (a document newly using it).
UPDATE tags
SET usage_count = usage_count + 1
WHERE id = $1;

-- name: DecrementTagUsage :exec
-- Lower a tag's counter by one, clamped at zero so a bookkeeping slip can never
-- drive the popularity counter negative (mislead autocomplete ranking).
UPDATE tags
SET usage_count = GREATEST(0, usage_count - 1)
WHERE id = $1;

-- name: GetTagByID :one
-- Resolve a tag id (admin tag-policy revoke) to its label for the confirm prompt.
SELECT id, slug, label, usage_count, created_at
FROM tags
WHERE id = $1;
