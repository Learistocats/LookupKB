-- +goose Up
-- Serves the keyset-paginated public catalogue (ListPublishedDocuments).
CREATE INDEX documents_published_idx ON documents (published_at DESC, id DESC)
    WHERE status = 'published' AND deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS documents_published_idx;
