-- Append-only activity trail (03-data-model.md §1 `audit_log`, 06-security.md §9).
-- The publish gate (LOO-14) writes a row for every publish/unpublish (AC-10.5);
-- ACL/admin actions add their own rows in later milestones. The app role holds
-- INSERT/SELECT only (00005_grant_app_role.sql) so entries are tamper-resistant —
-- there is deliberately no UPDATE/DELETE query here.

-- name: InsertAuditLog :exec
-- Record one audited action. actor_id is nullable (system actions have none);
-- target_id is text so it can key documents (uuid), groups, etc. metadata carries
-- action-specific context as jsonb (e.g. {"version_no":2,"first_publish":false}).
INSERT INTO audit_log (actor_id, action, target_type, target_id, metadata)
VALUES ($1, $2, $3, $4, $5);
