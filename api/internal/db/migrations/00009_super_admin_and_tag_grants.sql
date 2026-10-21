-- +goose Up
-- LOO-56: the super-admin role and the group tag-grant policy.
--
-- users.is_super_admin gates the /api/admin surface. It is read from this column on
-- EVERY request by the auth middleware (never baked into the JWT), so a revoke is
-- honored on the caller's next request (06-security.md, privilege escalation).
-- Bootstrapped with `api admin grant <email>`; the API refuses to demote the last
-- super-admin.
ALTER TABLE users ADD COLUMN is_super_admin boolean NOT NULL DEFAULT false;

-- group_tag_grants: a group's authorized tag policy (03-data-model.md §1). A tag
-- with at least one grant is a *protected* tag; enforcement (who may read/publish
-- entries carrying it) is LOO-33 — this migration only stores the policy.
CREATE TABLE group_tag_grants (
    group_id   uuid        NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    tag_id     uuid        NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, tag_id)
);
-- "Is this tag protected / which groups grant it" looks up by tag.
CREATE INDEX group_tag_grants_tag_id_idx ON group_tag_grants (tag_id);
-- Explicit (like 00007) rather than relying on 00005's default privileges, which
-- only cover tables created by the role that ran 00005.
GRANT SELECT, INSERT, UPDATE, DELETE ON group_tag_grants TO lookupkb_app;

-- +goose Down
DROP TABLE IF EXISTS group_tag_grants;
ALTER TABLE users DROP COLUMN is_super_admin;
