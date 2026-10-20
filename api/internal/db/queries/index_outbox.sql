-- Transactional outbox for Meilisearch index ops (03-data-model.md §4,
-- 00004_index_outbox.sql). The publish gate (LOO-14) enqueues a row in the SAME
-- transaction as the status flip, so a failed Meili write can be retried without
-- losing the DB commit and a rolled-back publish never leaves a stray index op.
-- The search worker (internal/search) claims, applies and completes them.

-- name: EnqueueIndexOp :exec
-- Enqueue an index operation for a document: 'upsert' on publish/re-publish,
-- 'delete' on unpublish. status defaults to 'pending' and attempts to 0 — the M2
-- worker scans (status,created_at) oldest-first.
INSERT INTO index_outbox (document_id, op)
VALUES ($1, $2);

-- name: ClaimIndexOps :many
-- Atomically claim the oldest pending ops for this worker. SKIP LOCKED lets
-- several workers run without double-processing a row.
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
-- Ops left 'processing' by a crashed worker go back to pending at startup.
UPDATE index_outbox
SET status = 'pending',
    updated_at = now()
WHERE status = 'processing';
