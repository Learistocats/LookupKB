-- +goose Up
-- Least-privilege grants for the runtime application role (06-security.md §8).
-- The `lookupkb_app` role is created outside migrations (deploy/postgres/init/
-- 20-lookupkb-roles.sh in prod; the test/db-reset bootstrap in CI) because GRANT
-- requires the objects to already exist. This migration runs as the schema owner
-- (the migration role) and hands the app role DML — never DDL, so it cannot
-- DROP/ALTER any table.
GRANT USAGE ON SCHEMA public TO lookupkb_app;

-- Full DML on the application tables the API reads and writes.
GRANT SELECT, INSERT, UPDATE, DELETE ON
    users, groups, documents, document_versions, tags, document_tags,
    document_acl, attachments, settings, api_tokens, index_outbox
    TO lookupkb_app;

-- audit_log is insert-only from the app's perspective (tamper-resistant): the app
-- may append and read, but never UPDATE or DELETE existing entries.
GRANT SELECT, INSERT ON audit_log TO lookupkb_app;

-- bigserial columns (audit_log, index_outbox) need sequence USAGE to allocate ids.
GRANT USAGE ON ALL SEQUENCES IN SCHEMA public TO lookupkb_app;

-- Future tables created by the migration role inherit the same DML defaults, so a
-- later migration that adds a table does not have to remember to grant.
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO lookupkb_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT USAGE ON SEQUENCES TO lookupkb_app;

-- +goose Down
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    REVOKE USAGE ON SEQUENCES FROM lookupkb_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    REVOKE SELECT, INSERT, UPDATE, DELETE ON TABLES FROM lookupkb_app;
REVOKE USAGE ON ALL SEQUENCES IN SCHEMA public FROM lookupkb_app;
REVOKE SELECT, INSERT ON audit_log FROM lookupkb_app;
REVOKE SELECT, INSERT, UPDATE, DELETE ON
    users, groups, documents, document_versions, tags, document_tags,
    document_acl, attachments, settings, api_tokens, index_outbox
    FROM lookupkb_app;
REVOKE USAGE ON SCHEMA public FROM lookupkb_app;
