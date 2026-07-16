-- +goose Up
-- Core content tables (03-data-model.md §1). Users and groups are lightweight
-- projections of Authentik identities — they hold NO authentication secrets.

-- users: projection of Authentik identities (03-data-model.md §1 `users`).
CREATE TABLE users (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    oidc_sub     text        NOT NULL UNIQUE,
    email        citext,
    display_name text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz
);

-- groups: projection of Authentik groups (03-data-model.md §1 `groups`).
-- Membership is NOT stored authoritatively here — it comes from the token claim.
CREATE TABLE groups (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    authentik_id text        NOT NULL UNIQUE,
    name         text        NOT NULL,
    synced_at    timestamptz
);

-- documents: current working copy + published cache (03-data-model.md §1 `documents`).
-- current_version_id FK is added after document_versions exists (cyclic reference).
CREATE TABLE documents (
    id             uuid            PRIMARY KEY DEFAULT gen_random_uuid(),
    slug           text            NOT NULL UNIQUE,
    author_id      uuid            NOT NULL REFERENCES users (id),
    status         document_status NOT NULL DEFAULT 'draft',
    title          text,
    description    text,
    source_md      text,
    rendered_html  text,
    access         document_access NOT NULL DEFAULT 'public',
    created_at     timestamptz     NOT NULL DEFAULT now(),
    updated_at     timestamptz     NOT NULL DEFAULT now(),
    published_at   timestamptz,
    current_version_id uuid,
    search_tsv     tsvector,
    -- Soft-delete marker (03-data-model.md §5). Purge job is a later ticket, but
    -- the column ships now so documents never needs a retrofit migration.
    deleted_at     timestamptz
);

-- document_versions: published snapshots only (03-data-model.md §1 `document_versions`).
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

-- Close the documents -> document_versions cycle now that both tables exist.
ALTER TABLE documents
    ADD CONSTRAINT documents_current_version_id_fkey
    FOREIGN KEY (current_version_id) REFERENCES document_versions (id);

-- tags: normalized tag vocabulary (03-data-model.md §1 `tags`).
CREATE TABLE tags (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    slug        text        NOT NULL UNIQUE,
    label       text        NOT NULL,
    usage_count int         NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- document_tags: M:N documents <-> tags (03-data-model.md §1 `document_tags`).
CREATE TABLE document_tags (
    document_id uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    tag_id      uuid NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    PRIMARY KEY (document_id, tag_id)
);

-- Hot-path indexes (03-data-model.md §4). slug is covered by its UNIQUE constraint.
CREATE INDEX documents_author_id_idx ON documents (author_id);
CREATE INDEX documents_status_idx ON documents (status);

-- +goose Down
DROP TABLE IF EXISTS document_tags;
DROP TABLE IF EXISTS tags;
ALTER TABLE documents DROP CONSTRAINT IF EXISTS documents_current_version_id_fkey;
DROP TABLE IF EXISTS document_versions;
DROP TABLE IF EXISTS documents;
DROP TABLE IF EXISTS groups;
DROP TABLE IF EXISTS users;
