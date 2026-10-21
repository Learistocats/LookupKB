-- +goose Up
-- LOO-58: drafts/unpublished documents can be hard-deleted. index_outbox rows must
-- survive that delete — a defensive 'delete' op enqueued in the same transaction as
-- the DELETE has to outlive the row so the worker can still purge the index. The
-- worker never needs the document row for a 'delete' op (and an 'upsert' whose
-- projection is gone already falls back to an index delete), so document_id
-- becomes a plain reference with no FK.
ALTER TABLE index_outbox DROP CONSTRAINT index_outbox_document_id_fkey;

-- +goose Down
-- Orphaned outbox rows (their document hard-deleted) would violate the restored
-- FK; they are history only, so drop them before re-adding the constraint.
DELETE FROM index_outbox o
WHERE NOT EXISTS (SELECT 1 FROM documents d WHERE d.id = o.document_id);
ALTER TABLE index_outbox
    ADD CONSTRAINT index_outbox_document_id_fkey
    FOREIGN KEY (document_id) REFERENCES documents (id) ON DELETE CASCADE;
