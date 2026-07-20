//go:build integration

package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	appdb "github.com/Learistocats/LookupKB/api/internal/db"
)

// TestUpsertUser proves the auth-middleware projection query: first call inserts,
// second call for the same oidc_sub updates in place (single row) refreshing
// email/display_name and bumping last_seen_at — never a second row, never a
// stored secret.
func TestUpsertUser(t *testing.T) {
	ctx := context.Background()

	pgc := startTestPostgres(t, ctx)

	dsn, err := pgc.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	// The schema's final migration grants to the least-privilege app role, so it
	// must exist first (bootstrapRoles is shared with the migration test).
	bootstrapRoles(t, ctx, dsn)

	// Superuser runs migrations here (this test focuses on the query, not roles).
	require.NoError(t, appdb.RunMigrations(ctx, dsn, "up"))

	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	q := dbgen.New(pool)

	email1, name1 := "a@example.com", "Ada"
	first, err := q.UpsertUser(ctx, dbgen.UpsertUserParams{
		OidcSub: "ak-sub-1", Email: &email1, DisplayName: &name1,
	})
	require.NoError(t, err)
	require.Equal(t, "ak-sub-1", first.OidcSub)
	require.Equal(t, email1, *first.Email)
	require.True(t, first.LastSeenAt.Valid)
	firstSeen := first.LastSeenAt.Time

	time.Sleep(10 * time.Millisecond) // ensure now() advances

	// Same subject, refreshed profile → update in place.
	email2, name2 := "ada@newmail.example", "Ada Lovelace"
	second, err := q.UpsertUser(ctx, dbgen.UpsertUserParams{
		OidcSub: "ak-sub-1", Email: &email2, DisplayName: &name2,
	})
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID, "same oidc_sub must reuse the same row")
	require.Equal(t, email2, *second.Email, "email refreshed from the token")
	require.Equal(t, name2, *second.DisplayName)
	require.True(t, second.LastSeenAt.Time.After(firstSeen), "last_seen_at must advance")

	// Exactly one row for the subject.
	var count int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT count(*) FROM users WHERE oidc_sub = $1", "ak-sub-1").Scan(&count))
	require.Equal(t, 1, count)

	// A user with no email (zero-groups-equivalent for the projection) is fine.
	noEmail, err := q.UpsertUser(ctx, dbgen.UpsertUserParams{
		OidcSub: "ak-sub-2", Email: nil, DisplayName: nil,
	})
	require.NoError(t, err)
	require.Nil(t, noEmail.Email)
}
