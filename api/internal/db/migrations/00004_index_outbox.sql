-- +goose Up
-- index_outbox: transactional outbox for Meilisearch index ops (03-data-model.md §4).
-- On publish/unpublish/edit-republish/ACL-change the app enqueues a row here in the
-- same transaction as the DB commit, so a failed Meili write can be retried without
-- losing the commit. Outbox PROCESSING (dequeue -> Meili upsert) is M2's ticket; the
-- table ships now so M2 needs no schema migration.
CREATE TABLE index_outbox (
    id           bigserial   PRIMARY KEY,
    document_id  uuid        NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    op           text        NOT NULL,            -- 'upsert' | 'delete'
    status       text        NOT NULL DEFAULT 'pending', -- pending|processing|done|failed
    attempts     int         NOT NULL DEFAULT 0,
    last_error   text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    processed_at timestamptz
);

-- Worker scan path: pick the oldest pending rows first.
CREATE INDEX index_outbox_status_created_at_idx ON index_outbox (status, created_at);

-- +goose Down
DROP TABLE IF EXISTS index_outbox;
