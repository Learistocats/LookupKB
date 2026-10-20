//go:build integration

package db_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	appdb "github.com/Learistocats/LookupKB/api/internal/db"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
)

// newFixture spins up Postgres, migrates, and returns a pool plus an author user
// id, the common preamble the publish-flow tests need.
func newFixture(t *testing.T, ctx context.Context) (*pgxpool.Pool, pgtype.UUID) {
	t.Helper()

	pgc := startTestPostgres(t, ctx)
	dsn, err := pgc.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	bootstrapRoles(t, ctx, dsn)
	require.NoError(t, appdb.RunMigrations(ctx, dsn, "up"))

	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	author, err := dbgen.New(pool).CreateUser(ctx, dbgen.CreateUserParams{Email: "author@example.com", PasswordHash: "x"})
	require.NoError(t, err)
	return pool, author.ID
}

func newDoc(t *testing.T, ctx context.Context, pool *pgxpool.Pool, author pgtype.UUID, slug string) pgtype.UUID {
	t.Helper()
	doc, err := dbgen.New(pool).CreateDraft(ctx, dbgen.CreateDraftParams{Slug: slug, AuthorID: author})
	require.NoError(t, err)
	return doc.ID
}
