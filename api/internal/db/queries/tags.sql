-- Tag slugs are normalized in Go (internal/tags.Normalize) before reaching these
-- queries. usage_count reflects live published usage and is maintained by the
-- increment/decrement queries via tags.ApplyDocumentTags.

-- name: SearchTags :many
-- Autocomplete, most-used first. The pattern is built in Go as '%slug%'; Normalize
-- strips LIKE metacharacters, so there is no injection risk. excluded_ids are the
-- protected tags the caller lacks; is_protected lets the publish dialog warn that
-- the entry will be restricted.
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
-- Get-or-create by slug. DO UPDATE (not DO NOTHING) so the existing row is returned.
-- usage_count is deliberately untouched; see Increment/DecrementTagUsage.
INSERT INTO tags (slug, label)
VALUES ($1, $2)
ON CONFLICT (slug) DO UPDATE
SET label = EXCLUDED.label
RETURNING id, slug, label, usage_count, created_at;

-- name: ListDocumentTags :many
-- Diff source when re-applying a document's tag set.
SELECT t.id, t.slug
FROM document_tags dt
JOIN tags t ON t.id = dt.tag_id
WHERE dt.document_id = $1;

-- name: AddDocumentTag :exec
-- Idempotent; the usage_count increment is issued separately for genuinely new tags.
INSERT INTO document_tags (document_id, tag_id)
VALUES ($1, $2)
ON CONFLICT (document_id, tag_id) DO NOTHING;

-- name: RemoveDocumentTag :exec
-- Paired with DecrementTagUsage in the same tx.
DELETE FROM document_tags
WHERE document_id = $1
  AND tag_id = $2;

-- name: IncrementTagUsage :exec
UPDATE tags
SET usage_count = usage_count + 1
WHERE id = $1;

-- name: DecrementTagUsage :exec
-- Clamped at zero so a bookkeeping slip can never go negative.
UPDATE tags
SET usage_count = GREATEST(0, usage_count - 1)
WHERE id = $1;

-- name: GetTagByID :one
-- Resolves a tag id to its label for the admin confirm prompt.
SELECT id, slug, label, usage_count, created_at
FROM tags
WHERE id = $1;
