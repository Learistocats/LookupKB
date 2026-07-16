-- +goose Up
-- Extensions: citext for case-insensitive user emails (03-data-model.md §1 `users`),
-- pgcrypto for gen_random_uuid() UUID primary keys (available in pg16 core too).
CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Document lifecycle and access enums (03-data-model.md §1 `documents`).
CREATE TYPE document_status AS ENUM ('draft', 'published', 'unpublished');
CREATE TYPE document_access AS ENUM ('public', 'restricted');

-- +goose Down
DROP TYPE IF EXISTS document_access;
DROP TYPE IF EXISTS document_status;
-- Extensions are left in place: they are cluster-wide and may be shared; dropping
-- citext would fail while any column still uses it. `goose down` on 00001 only
-- reverses this migration's types.
