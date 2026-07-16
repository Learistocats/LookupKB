-- +goose Up
-- Access control, attachments, audit, settings and PATs (03-data-model.md §1).

-- document_acl: restricted docs -> allowed groups (03-data-model.md §1 `document_acl`).
-- Empty for public documents; OR-semantics across groups.
CREATE TABLE document_acl (
    document_id uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    group_id    uuid NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    PRIMARY KEY (document_id, group_id)
);

-- attachments: content-addressed blobs in Garage (03-data-model.md §1 `attachments`).
-- document_id is nullable while drafting; SET NULL keeps orphaned blobs for the
-- purge job rather than cascading the delete.
CREATE TABLE attachments (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id  uuid        REFERENCES documents (id) ON DELETE SET NULL,
    owner_id     uuid        NOT NULL REFERENCES users (id),
    s3_key       text        NOT NULL,
    content_type text        NOT NULL,
    size_bytes   bigint      NOT NULL,
    sha256       text        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

-- audit_log: append-only activity trail (03-data-model.md §1 `audit_log`).
-- actor_id is nullable with no FK: rows may record system actions (null) or
-- actors that predate their users projection. The app role is granted
-- INSERT/SELECT only (00005) to keep this tamper-resistant.
CREATE TABLE audit_log (
    id          bigserial   PRIMARY KEY,
    actor_id    uuid,
    action      text        NOT NULL,
    target_type text        NOT NULL,
    target_id   text,
    metadata    jsonb       NOT NULL DEFAULT '{}',
    at          timestamptz NOT NULL DEFAULT now()
);

-- settings: admin-tunable key/value limits (03-data-model.md §1 `settings`).
CREATE TABLE settings (
    key        text        PRIMARY KEY,
    value      jsonb       NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- api_tokens: Personal Access Tokens for the public API (03-data-model.md §1 `api_tokens`).
-- token_hash is the ONLY persisted form of the secret — never add a plaintext or
-- reversibly-encrypted column (06-security.md, ticket "things not to miss").
CREATE TABLE api_tokens (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         text        NOT NULL,
    token_hash   text        NOT NULL,
    prefix       text        NOT NULL,
    scopes       text[]      NOT NULL DEFAULT '{}',
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    expires_at   timestamptz,
    revoked_at   timestamptz
);

-- Hot-path indexes (03-data-model.md §4).
CREATE INDEX document_acl_group_id_idx ON document_acl (group_id);
CREATE INDEX audit_log_at_idx ON audit_log (at);

-- +goose Down
DROP TABLE IF EXISTS api_tokens;
DROP TABLE IF EXISTS settings;
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS attachments;
DROP TABLE IF EXISTS document_acl;
