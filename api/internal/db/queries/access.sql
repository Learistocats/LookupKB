-- Protected-tag access policy (LOO-33, 03-data-model.md §2). A tag is *protected*
-- iff at least one group_tag_grants row references it; a caller is *granted* a
-- protected tag iff they belong to a group that grants it. This file is the ONE
-- place the denied set is defined in SQL; internal/access turns it into the read /
-- publish / autocomplete / search decisions.

-- name: ListDeniedProtectedTags :many
-- The protected tags the user is NOT granted. Loaded once per request that needs
-- an access decision, straight from group_members + group_tag_grants, so a policy
-- or membership change is honored on the very next request (no reindex, no token
-- refresh). Super-admins never reach this query (access.Load short-circuits).
SELECT t.id, t.slug, t.label
FROM tags t
WHERE EXISTS (SELECT 1 FROM group_tag_grants g WHERE g.tag_id = t.id)
  AND NOT EXISTS (
      SELECT 1
      FROM group_tag_grants g
      JOIN group_members m ON m.group_id = g.group_id
      WHERE g.tag_id = t.id
        AND m.user_id = $1
  )
ORDER BY t.slug ASC;
