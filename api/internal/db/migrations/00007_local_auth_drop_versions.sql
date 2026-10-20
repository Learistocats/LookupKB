-- +goose Up
-- Lightweight revamp: Authentik is gone, so LookupKB owns its credentials and its
-- group membership; published-version snapshots are dropped (re-publish simply
-- overwrites documents.rendered_html).

-- users: no longer an OIDC projection. email is the login identifier and
-- password_hash holds an argon2id PHC string — the ONLY persisted form of the
-- password (never plaintext or reversibly encrypted).
-- Pre-existing OIDC projections without an email get an unusable placeholder; with
-- an empty password_hash they can never log in.
ALTER TABLE users DROP COLUMN oidc_sub;
UPDATE users SET email = id::text || '@invalid.localhost' WHERE email IS NULL;
ALTER TABLE users ALTER COLUMN email SET NOT NULL;
ALTER TABLE users ADD CONSTRAINT users_email_key UNIQUE (email);
ALTER TABLE users ADD COLUMN password_hash text NOT NULL DEFAULT '';
ALTER TABLE users ALTER COLUMN password_hash DROP DEFAULT;

-- groups: local, named groups. Membership is authoritative here now that there is
-- no token claim to carry it.
ALTER TABLE groups DROP COLUMN authentik_id;
ALTER TABLE groups DROP COLUMN synced_at;
ALTER TABLE groups ADD CONSTRAINT groups_name_key UNIQUE (name);
ALTER TABLE groups ADD COLUMN created_at timestamptz NOT NULL DEFAULT now();

CREATE TABLE group_members (
    group_id uuid NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    user_id  uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, user_id)
);
CREATE INDEX group_members_user_id_idx ON group_members (user_id);
GRANT SELECT, INSERT, UPDATE, DELETE ON group_members TO lookupkb_app;

-- Version snapshots are gone: the documents row is the only copy of a document.
ALTER TABLE documents DROP CONSTRAINT documents_current_version_id_fkey;
ALTER TABLE documents DROP COLUMN current_version_id;
DROP TABLE document_versions;

-- +goose Down
CREATE TABLE document_versions (
    id            uuid            PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id   uuid            NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    version_no    int             NOT NULL,
    title         text,
    description   text,
    source_md     text,
    rendered_html text,
    tags          text[],
    access        document_access NOT NULL,
    acl_group_ids uuid[],
    published_by  uuid            REFERENCES users (id),
    published_at  timestamptz     NOT NULL DEFAULT now(),
    UNIQUE (document_id, version_no)
);
GRANT SELECT, INSERT ON document_versions TO lookupkb_app;
ALTER TABLE documents ADD COLUMN current_version_id uuid;
ALTER TABLE documents
    ADD CONSTRAINT documents_current_version_id_fkey
    FOREIGN KEY (current_version_id) REFERENCES document_versions (id);

DROP TABLE IF EXISTS group_members;
ALTER TABLE groups DROP COLUMN created_at;
ALTER TABLE groups DROP CONSTRAINT groups_name_key;
ALTER TABLE groups ADD COLUMN synced_at timestamptz;
ALTER TABLE groups ADD COLUMN authentik_id text;
UPDATE groups SET authentik_id = id::text;
ALTER TABLE groups ALTER COLUMN authentik_id SET NOT NULL;
ALTER TABLE groups ADD CONSTRAINT groups_authentik_id_key UNIQUE (authentik_id);

ALTER TABLE users DROP COLUMN password_hash;
ALTER TABLE users DROP CONSTRAINT users_email_key;
ALTER TABLE users ALTER COLUMN email DROP NOT NULL;
ALTER TABLE users ADD COLUMN oidc_sub text;
UPDATE users SET oidc_sub = id::text;
ALTER TABLE users ALTER COLUMN oidc_sub SET NOT NULL;
ALTER TABLE users ADD CONSTRAINT users_oidc_sub_key UNIQUE (oidc_sub);
