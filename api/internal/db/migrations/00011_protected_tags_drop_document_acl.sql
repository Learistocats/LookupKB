-- +goose Up
-- LOO-33: protected tags replace the per-document ACL.
--
-- Read access to a published entry is now derived from its tags and the group tag
-- policy (group_tag_grants, 00009): a tag is *protected* iff at least one group
-- grants it, and a caller reads a published entry iff they are a super-admin, its
-- author, or granted EVERY protected tag it carries (03-data-model.md §2). There is
-- no per-document access flag or group list any more, so both go.
DROP TABLE IF EXISTS document_acl;
ALTER TABLE documents DROP COLUMN access;
-- documents.access was the enum's last user (document_versions went in 00007).
DROP TYPE IF EXISTS document_access;

-- The search index documents still carry the dropped `access`/`acl_group_ids`
-- attributes. Re-project every published entry (drafts are never indexed) so the
-- worker replaces them with the current, smaller projection.
INSERT INTO index_outbox (document_id, op)
SELECT id, 'upsert'
FROM documents
WHERE status = 'published'
  AND deleted_at IS NULL;

-- +goose Down
-- Recreate the pre-LOO-33 shape (empty: every entry back to 'public') so the chain
-- reverses cleanly; the policy that replaced it lives on in group_tag_grants.
CREATE TYPE document_access AS ENUM ('public', 'restricted');
ALTER TABLE documents ADD COLUMN access document_access NOT NULL DEFAULT 'public';
CREATE TABLE document_acl (
    document_id uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    group_id    uuid NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    PRIMARY KEY (document_id, group_id)
);
CREATE INDEX document_acl_group_id_idx ON document_acl (group_id);
GRANT SELECT, INSERT, UPDATE, DELETE ON document_acl TO lookupkb_app;
