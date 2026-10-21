-- +goose Up
-- Personal Access Tokens (LOO-37). A PAT is `lkb_pat_<prefix>_<secret>`; the
-- non-secret prefix is the lookup key on every PAT-authenticated request, so it
-- must identify exactly one row. A UNIQUE index gives both the guarantee and the
-- hot-path index (the issuer retries on the astronomically rare collision).
CREATE UNIQUE INDEX api_tokens_prefix_key ON api_tokens (prefix);

-- The settings page lists (and the issuer counts) a user's own tokens.
CREATE INDEX api_tokens_user_id_idx ON api_tokens (user_id);

-- +goose Down
DROP INDEX IF EXISTS api_tokens_user_id_idx;
DROP INDEX IF EXISTS api_tokens_prefix_key;
