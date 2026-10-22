-- Local accounts. password_hash is an argon2id PHC string from internal/auth.

-- name: CreateUser :one
-- Register a new account. email is citext + UNIQUE, so a duplicate (any casing)
-- fails with a unique violation the signup handler maps to a generic conflict.
INSERT INTO users (email, password_hash, display_name)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetUserByEmail :one
-- Login lookup. citext makes the match case-insensitive.
SELECT *
FROM users
WHERE email = $1;

-- name: TouchUserLastSeen :exec
-- Bump last_seen_at on a successful login.
UPDATE users
SET last_seen_at = now()
WHERE id = $1;

-- ── Super-admin role ─────────────────────────────────────────────────────────

-- name: GetUserIsSuperAdmin :one
-- Loaded by the auth middleware on every request (never a token claim), so a
-- revoke takes effect on the caller's next request.
SELECT is_super_admin
FROM users
WHERE id = $1;

-- name: ListUsers :many
-- The admin user list. Never selects password_hash.
SELECT id, email, display_name, is_super_admin, created_at
FROM users
ORDER BY email ASC;

-- name: SetUserSuperAdmin :one
UPDATE users
SET is_super_admin = $2
WHERE id = $1
RETURNING id, email, display_name, is_super_admin, created_at;

-- name: SetUserSuperAdminByEmail :one
-- CLI bootstrap (`api admin grant|revoke <email>`). citext: case-insensitive.
UPDATE users
SET is_super_admin = $2
WHERE email = $1
RETURNING id, email, display_name, is_super_admin, created_at;

-- name: CountSuperAdmins :one
SELECT count(*)::bigint
FROM users
WHERE is_super_admin;

-- name: LockSuperAdmins :many
-- Row-locks every current super-admin inside the demotion transaction so two
-- concurrent demotions serialize: the second re-evaluates after the first commits
-- and sees one fewer admin, so the last-super-admin guard cannot be raced.
SELECT id
FROM users
WHERE is_super_admin
ORDER BY id
FOR UPDATE;
