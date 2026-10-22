//go:build integration

package db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
)

// TestAPITokens exercises the PAT queries end to end against a migrated Postgres:
// create (+ audit), active lookup, unique prefix, expiry/revocation filtering,
// owner-scoped revoke (+ audit), the throttled last_used_at stamp and the list.
func TestAPITokens(t *testing.T) {
	ctx := context.Background()
	pool, owner := newFixture(t, ctx)
	q := dbgen.New(pool)

	other, err := q.CreateUser(ctx, dbgen.CreateUserParams{Email: "other@example.com", PasswordHash: "x"})
	require.NoError(t, err)

	pat, err := auth.GeneratePAT()
	require.NoError(t, err)
	created, err := q.CreateAPIToken(ctx, dbgen.CreateAPITokenParams{
		UserID: owner, Name: "ci", TokenHash: pat.Hash, Prefix: pat.Prefix,
	})
	require.NoError(t, err)
	require.Equal(t, "ci", created.Name)
	require.False(t, created.RevokedAt.Valid)

	// Audit row written by the same statement, with no secret material.
	var action, meta string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT action, metadata::text FROM audit_log WHERE target_type = 'api_token' AND target_id = $1`,
		uuidText(created.ID)).Scan(&action, &meta))
	require.Equal(t, "token.create", action)
	require.Contains(t, meta, pat.Prefix)
	require.NotContains(t, meta, pat.Hash)
	require.NotContains(t, meta, pat.Token)

	// Lookup by prefix joins the owner's email.
	got, err := q.GetActiveAPITokenByPrefix(ctx, pat.Prefix)
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, pat.Hash, got.TokenHash)
	require.Equal(t, "author@example.com", got.Email)

	// The prefix is UNIQUE.
	_, err = q.CreateAPIToken(ctx, dbgen.CreateAPITokenParams{
		UserID: other.ID, Name: "dup", TokenHash: "other", Prefix: pat.Prefix,
	})
	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr))
	require.Equal(t, "23505", pgErr.Code)

	// An expired token is not active.
	expired, err := auth.GeneratePAT()
	require.NoError(t, err)
	_, err = q.CreateAPIToken(ctx, dbgen.CreateAPITokenParams{
		UserID: owner, Name: "old", TokenHash: expired.Hash, Prefix: expired.Prefix,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true},
	})
	require.NoError(t, err)
	_, err = q.GetActiveAPITokenByPrefix(ctx, expired.Prefix)
	require.ErrorIs(t, err, pgx.ErrNoRows)

	n, err := q.CountActiveAPITokens(ctx, owner)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)

	// last_used_at is stamped (throttling lives in the auth middleware).
	require.NoError(t, q.TouchAPITokenLastUsed(ctx, created.ID))
	first, err := q.GetActiveAPITokenByPrefix(ctx, pat.Prefix)
	require.NoError(t, err)
	require.True(t, first.LastUsedAt.Valid)

	// Revoke is owner-scoped: another user cannot, the owner can, once.
	_, err = q.RevokeMyAPIToken(ctx, dbgen.RevokeMyAPITokenParams{ID: created.ID, UserID: other.ID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = q.RevokeMyAPIToken(ctx, dbgen.RevokeMyAPITokenParams{ID: created.ID, UserID: owner})
	require.NoError(t, err)
	_, err = q.RevokeMyAPIToken(ctx, dbgen.RevokeMyAPITokenParams{ID: created.ID, UserID: owner})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = q.GetActiveAPITokenByPrefix(ctx, pat.Prefix)
	require.ErrorIs(t, err, pgx.ErrNoRows)

	var revokes int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_log WHERE action = 'token.revoke' AND target_id = $1`,
		uuidText(created.ID)).Scan(&revokes))
	require.Equal(t, 1, revokes, "only the effective revoke is audited")

	// List: own tokens only, revoked included.
	mine, err := q.ListMyAPITokens(ctx, owner)
	require.NoError(t, err)
	require.Len(t, mine, 2)
	theirs, err := q.ListMyAPITokens(ctx, other.ID)
	require.NoError(t, err)
	require.Empty(t, theirs)
}

func uuidText(u pgtype.UUID) string {
	v, _ := u.Value()
	return v.(string)
}
