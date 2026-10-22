-- Personal Access Tokens (03-data-model.md §1 `api_tokens`, 10-public-api.md §2).
-- token_hash is the hex SHA-256 of the full token — the ONLY persisted form of the
-- secret. No query here returns token_hash except the verifier's lookup.

-- name: CreateAPIToken :one
-- Issue a token for its owner and record the audit row in the same statement (one
-- implicit transaction). The audit metadata carries the display prefix and name,
-- never the secret or its hash. The per-user cap is enforced in the INSERT itself:
-- at the cap nothing is inserted and the query returns no row (pgx.ErrNoRows).
WITH t AS (
    INSERT INTO api_tokens (user_id, name, token_hash, prefix, expires_at)
    SELECT @user_id::uuid, @name::text, @token_hash::text, @prefix::text, @expires_at::timestamptz
    WHERE (
        SELECT count(*)
        FROM api_tokens
        WHERE user_id = @user_id::uuid
          AND revoked_at IS NULL
          AND (expires_at IS NULL OR expires_at > now())
    ) < @max_active::bigint
    RETURNING id, user_id, name, prefix, created_at, last_used_at, expires_at, revoked_at
), a AS (
    INSERT INTO audit_log (actor_id, action, target_type, target_id, metadata)
    SELECT t.user_id, 'token.create', 'api_token', t.id::text,
           jsonb_build_object('prefix', t.prefix, 'name', t.name)
    FROM t
)
SELECT id, user_id, name, prefix, created_at, last_used_at, expires_at, revoked_at
FROM t;

-- name: GetActiveAPITokenByPrefix :one
-- The verifier's lookup: an active token by its non-secret prefix, with the owner's
-- email. Revoked/expired tokens yield no row. The caller still compares token_hash
-- in constant time (the prefix alone proves nothing).
SELECT t.id, t.user_id, t.token_hash, t.expires_at, t.revoked_at, t.last_used_at, u.email
FROM api_tokens t
JOIN users u ON u.id = t.user_id
WHERE t.prefix = $1
  AND t.revoked_at IS NULL
  AND (t.expires_at IS NULL OR t.expires_at > now());

-- name: ListMyAPITokens :many
-- The caller's tokens for the settings page, newest first. Never the hash.
SELECT id, name, prefix, created_at, last_used_at, expires_at, revoked_at
FROM api_tokens
WHERE user_id = $1
ORDER BY created_at DESC, id;

-- name: RevokeMyAPIToken :one
-- Owner-scoped revoke with its audit row in the same statement. A missing, foreign
-- or already-revoked token matches nothing (pgx.ErrNoRows ⇒ identical 404).
WITH r AS (
    UPDATE api_tokens
    SET revoked_at = now()
    WHERE api_tokens.id = @id
      AND api_tokens.user_id = @user_id
      AND api_tokens.revoked_at IS NULL
    RETURNING api_tokens.id, api_tokens.user_id, api_tokens.prefix
), a AS (
    INSERT INTO audit_log (actor_id, action, target_type, target_id, metadata)
    SELECT r.user_id, 'token.revoke', 'api_token', r.id::text,
           jsonb_build_object('prefix', r.prefix)
    FROM r
)
SELECT id FROM r;

-- name: TouchAPITokenLastUsed :exec
-- Record usage, throttled: only writes when last_used_at is unset or older than a
-- minute, so a busy token does not cost a row write per request.
UPDATE api_tokens
SET last_used_at = now()
WHERE id = $1
  AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute');
