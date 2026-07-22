-- Admin-tunable key/value limits (03-data-model.md §1 `settings`). This is the
-- first reader of the settings table; the metadata-gate ticket (LOO-14) adds
-- more keys (title/description limits) alongside the tag limits key LOO-15 uses.

-- name: GetSetting :one
-- Fetch a single settings value (jsonb) by key. Returns pgx.ErrNoRows when the
-- key is unset, which callers treat as "use defaults" (see tags.LoadLimits).
SELECT value
FROM settings
WHERE key = $1;
