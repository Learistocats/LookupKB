//go:build integration

package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

// TestUsers covers the local-account queries: create, case-insensitive lookup and
// uniqueness (citext), last_seen bump, and group membership listing.
func TestUsers(t *testing.T) {
	ctx := context.Background()
	pool, _ := newFixture(t, ctx)
	q := dbgen.New(pool)

	name := "Ada"
	created, err := q.CreateUser(ctx, dbgen.CreateUserParams{
		Email: "Ada@Example.com", PasswordHash: "$argon2id$stub", DisplayName: &name,
	})
	require.NoError(t, err)
	require.False(t, created.LastSeenAt.Valid)

	// citext: lookup ignores case.
	got, err := q.GetUserByEmail(ctx, "ada@example.COM")
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, "$argon2id$stub", got.PasswordHash)

	_, err = q.GetUserByEmail(ctx, "nobody@example.com")
	require.ErrorIs(t, err, pgx.ErrNoRows)

	// UNIQUE(email) is case-insensitive too.
	_, err = q.CreateUser(ctx, dbgen.CreateUserParams{Email: "ADA@example.com", PasswordHash: "x"})
	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr))
	require.Equal(t, "23505", pgErr.Code)

	require.NoError(t, q.TouchUserLastSeen(ctx, created.ID))
	byID, err := q.GetUserByID(ctx, created.ID)
	require.NoError(t, err)
	require.True(t, byID.LastSeenAt.Valid)

	// Group membership.
	gids, err := q.ListUserGroupIDs(ctx, created.ID)
	require.NoError(t, err)
	require.Empty(t, gids)

	var groupID [16]byte
	require.NoError(t, pool.QueryRow(ctx, "INSERT INTO groups (name) VALUES ('ops') RETURNING id").Scan(&groupID))
	_, err = pool.Exec(ctx, "INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)", groupID, created.ID)
	require.NoError(t, err)

	gids, err = q.ListUserGroupIDs(ctx, created.ID)
	require.NoError(t, err)
	require.Len(t, gids, 1)
	require.Equal(t, groupID, gids[0].Bytes)
}
