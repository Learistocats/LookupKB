-- Local email/password accounts (03-data-model.md §1 `users`). password_hash is an
-- argon2id PHC string produced by internal/auth — never plaintext.

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

-- name: GetUserByID :one
SELECT *
FROM users
WHERE id = $1;

-- name: TouchUserLastSeen :exec
-- Bump last_seen_at on a successful login.
UPDATE users
SET last_seen_at = now()
WHERE id = $1;
