-- Users are a projection of Authentik identities (03-data-model.md §1). The auth
-- middleware upserts a row on each verified request, keyed on the stable OIDC
-- `sub`. NO authentication secret is ever stored — only display projections.

-- name: UpsertUser :one
-- Insert-or-refresh the caller's projection on login/request. oidc_sub is the
-- stable Authentik subject and the conflict target; email/display_name are
-- refreshed from the verified token and last_seen_at bumped to now().
INSERT INTO users (oidc_sub, email, display_name, last_seen_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (oidc_sub) DO UPDATE
SET email        = EXCLUDED.email,
    display_name = EXCLUDED.display_name,
    last_seen_at = now()
RETURNING *;
