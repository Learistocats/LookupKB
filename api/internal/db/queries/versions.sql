-- Published-version snapshots (LOO-17, F9 storage). Only *published* documents are
-- snapshotted: every publish/re-publish appends an immutable document_versions row
-- (03-data-model.md §document_versions, 00-overview.md §8). Draft saves are NOT
-- versioned — they overwrite documents.source_md in place (see documents.sql).
-- These queries are composed by the publish-gate transaction (LOO-14); the status /
-- published_at transition itself is the gate's job, not this ticket's.

-- name: LockDocumentForPublish :one
-- Take a row lock on the document at the start of the publish transaction so two
-- concurrent (re)publishes of the same document can't both read the same
-- MAX(version_no) and race into a UNIQUE(document_id, version_no) violation. Also
-- confirms the document exists and isn't soft-deleted (pgx.ErrNoRows otherwise).
SELECT id
FROM documents
WHERE id = $1
  AND deleted_at IS NULL
FOR UPDATE;

-- name: CreateDocumentVersion :one
-- Append the next immutable snapshot for a document. version_no is monotonic per
-- document (1,2,3…), computed as MAX+1 under the row lock above. Every column is a
-- point-in-time COPY of the just-validated/just-rendered document — denormalized on
-- purpose (no FK back to live tags/documents) so the snapshot stays correct even
-- after the live document later changes.
INSERT INTO document_versions (
    document_id,
    version_no,
    title,
    description,
    source_md,
    rendered_html,
    tags,
    access,
    acl_group_ids,
    published_by
)
VALUES (
    @document_id,
    COALESCE((SELECT MAX(version_no) FROM document_versions WHERE document_id = @document_id), 0) + 1,
    @title,
    @description,
    @source_md,
    @rendered_html,
    @tags,
    @access,
    @acl_group_ids,
    @published_by
)
RETURNING *;

-- name: SetCurrentVersion :exec
-- Repoint the document at its latest published snapshot. Runs in the same publish
-- transaction as CreateDocumentVersion, so a partial failure can't leave
-- current_version_id dangling or stale.
UPDATE documents
SET current_version_id = $2,
    updated_at         = now()
WHERE id = $1;

-- name: GetDocumentVersions :many
-- Internal read path for the future M5 history UI: a document's snapshots, newest
-- first. No access check here — the caller (public API / M5) applies it.
SELECT *
FROM document_versions
WHERE document_id = $1
ORDER BY version_no DESC;
