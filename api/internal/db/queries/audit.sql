-- Append-only: the app role holds INSERT/SELECT only, so there is deliberately no
-- UPDATE/DELETE query here.

-- name: InsertAuditLog :exec
-- actor_id is nullable (system actions have none); target_id is text so it can key
-- documents, groups, users, etc.
INSERT INTO audit_log (actor_id, action, target_type, target_id, metadata)
VALUES ($1, $2, $3, $4, $5);
