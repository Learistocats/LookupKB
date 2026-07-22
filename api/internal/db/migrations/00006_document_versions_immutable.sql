-- +goose Up
-- Published-version snapshots are immutable (00-overview.md §8; 03-data-model.md
-- §document_versions; LOO-17). Enforce it at the DB level, not just in application
-- code: the runtime app role may APPEND (INSERT) and READ (SELECT) snapshots but
-- must never mutate one — exactly like the audit_log append-only grant in
-- 00005_grant_app_role.sql. This revokes the UPDATE/DELETE that 00005's blanket
-- grant handed the app role for this table.
--
-- ON DELETE CASCADE from documents still works: Postgres runs referential actions
-- as the table owner, bypassing the app role's revoked privilege, so soft-delete /
-- purge of a document still cascades away its snapshots.
REVOKE UPDATE, DELETE ON document_versions FROM lookupkb_app;

-- +goose Down
GRANT UPDATE, DELETE ON document_versions TO lookupkb_app;
