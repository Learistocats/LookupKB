-- name: EnqueueIndexOp :exec
-- 'upsert' on publish/re-publish, 'delete' on unpublish.
INSERT INTO index_outbox (document_id, op)
VALUES ($1, $2);

-- name: ClaimIndexOps :many
-- SKIP LOCKED lets several workers run without double-processing a row.
UPDATE index_outbox
SET status = 'processing',
    updated_at = now()
WHERE id IN (
    SELECT o.id
    FROM index_outbox o
    WHERE o.status = 'pending'
    ORDER BY o.created_at
    FOR UPDATE SKIP LOCKED
    LIMIT $1
)
RETURNING id, document_id, op, attempts;

-- name: CompleteIndexOp :exec
UPDATE index_outbox
SET status = 'done',
    processed_at = now(),
    updated_at = now(),
    last_error = NULL
WHERE id = $1;

-- name: FailIndexOp :exec
-- Record a failure: back to pending for a retry, or 'failed' once attempts run out.
UPDATE index_outbox
SET attempts = attempts + 1,
    last_error = @last_error,
    status = CASE WHEN attempts + 1 >= @max_attempts::int THEN 'failed' ELSE 'pending' END,
    updated_at = now()
WHERE id = @id;

-- name: RequeueStaleIndexOps :exec
-- Ops left 'processing' by a crashed worker.
UPDATE index_outbox
SET status = 'pending',
    updated_at = now()
WHERE status = 'processing';

-- name: EnqueueAllPublishedUpserts :execrows
-- Index bootstrap: queue an upsert for every published document.
INSERT INTO index_outbox (document_id, op)
SELECT id, 'upsert'
FROM documents
WHERE status = 'published'
  AND deleted_at IS NULL;
