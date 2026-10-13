-- Local groups (03-data-model.md §1 `groups`, `group_members`). Membership is
-- authoritative here; it feeds the per-request ACL decision (M3).

-- name: ListUserGroupIDs :many
-- The caller's group ids, loaded by the auth middleware on every request so an
-- access decision never relies on a stale token claim.
SELECT group_id
FROM group_members
WHERE user_id = $1;
