-- Local groups (03-data-model.md §1 `groups`, `group_members`). Membership is
-- authoritative here; it feeds the per-request ACL decision (M3).

-- name: ListUserGroupIDs :many
-- The caller's group ids, loaded by the auth middleware on every request so an
-- access decision never relies on a stale token claim.
SELECT group_id
FROM group_members
WHERE user_id = $1;

-- ── Admin group management (LOO-56, F10) ─────────────────────────────────────
-- Every mutation below is called by internal/admin inside a transaction that also
-- writes the matching audit_log row.

-- name: CreateGroup :one
-- groups.name is UNIQUE (00007): a duplicate surfaces as a 23505 the admin service
-- maps to a 409.
INSERT INTO groups (name)
VALUES ($1)
RETURNING id, name, created_at;

-- name: RenameGroup :one
UPDATE groups
SET name = $2
WHERE id = $1
RETURNING id, name, created_at;

-- name: DeleteGroup :execrows
-- Cascades to group_members, group_tag_grants and document_acl.
DELETE FROM groups
WHERE id = $1;

-- name: GetGroup :one
SELECT id, name, created_at
FROM groups
WHERE id = $1;

-- name: ListGroups :many
-- The admin overview: every group with its member and tag-grant counts.
SELECT g.id,
       g.name,
       g.created_at,
       (SELECT count(*) FROM group_members gm WHERE gm.group_id = g.id)::bigint    AS member_count,
       (SELECT count(*) FROM group_tag_grants gt WHERE gt.group_id = g.id)::bigint AS tag_count
FROM groups g
ORDER BY g.name ASC;

-- name: ListGroupMembers :many
SELECT u.id, u.email, u.display_name, u.is_super_admin
FROM group_members gm
JOIN users u ON u.id = gm.user_id
WHERE gm.group_id = $1
ORDER BY u.email ASC;

-- name: AddGroupMember :execrows
-- Idempotent: re-adding an existing member affects 0 rows (no duplicate audit).
INSERT INTO group_members (group_id, user_id)
VALUES ($1, $2)
ON CONFLICT (group_id, user_id) DO NOTHING;

-- name: RemoveGroupMember :execrows
DELETE FROM group_members
WHERE group_id = $1
  AND user_id = $2;

-- ── Group tag policy (group_tag_grants) ──────────────────────────────────────
-- A tag with >= 1 grant is a protected tag. Enforcement is LOO-33; these queries
-- only maintain the policy.

-- name: ListGroupTagGrants :many
SELECT t.id AS tag_id, t.slug, t.label, gt.created_at
FROM group_tag_grants gt
JOIN tags t ON t.id = gt.tag_id
WHERE gt.group_id = $1
ORDER BY t.slug ASC;

-- name: AddGroupTagGrant :execrows
-- Idempotent: re-granting an existing pair affects 0 rows.
INSERT INTO group_tag_grants (group_id, tag_id)
VALUES ($1, $2)
ON CONFLICT (group_id, tag_id) DO NOTHING;

-- name: RemoveGroupTagGrant :execrows
DELETE FROM group_tag_grants
WHERE group_id = $1
  AND tag_id = $2;

-- name: CountTagGrants :one
-- How many groups grant this tag: 0 means the tag is public, >= 1 protected. The
-- admin confirm flow uses it to detect "first grant" / "last grant".
SELECT count(*)::bigint
FROM group_tag_grants
WHERE tag_id = $1;
