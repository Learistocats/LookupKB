-- name: GetSettings :many
-- Settings rows for the given keys; unset keys are simply absent (callers use defaults).
SELECT key, value
FROM settings
WHERE key = ANY (@keys::text[]);
