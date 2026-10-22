//go:build integration

package db_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	appdb "github.com/Learistocats/LookupKB/api/internal/db"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	appPassword      = "app-secret"
	migratorPassword = "migrator-secret"
)

// startTestPostgres boots a throwaway Postgres for the integration tests and
// registers its teardown. It waits for the "ready to accept connections" log to
// appear TWICE, not just for the port to open: the postgres entrypoint starts a
// temporary server to run init, then restarts. A port-only (or first-log) wait
// returns during that init window, so a connect can race the restart and get
// "connection reset by peer" — the flake this strategy removes.
func startTestPostgres(t *testing.T, ctx context.Context) *postgres.PostgresContainer {
	t.Helper()
	pgc, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("lookupkb"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pgc.Terminate(ctx) })
	return pgc
}

// TestMigrations proves the schema applies cleanly up and down against a fresh
// Postgres, and that the least-privilege app role cannot run DDL — the ticket's
// two hard acceptance criteria (clean up/down; app role cannot DROP/ALTER).
func TestMigrations(t *testing.T) {
	ctx := context.Background()

	pgc := startTestPostgres(t, ctx)

	superDSN, err := pgc.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	// Mirror deploy/postgres/init/20-lookupkb-roles.sh: the roles are provisioned
	// by infra before migrations run (a bare testcontainers image has no init dir).
	bootstrapRoles(t, ctx, superDSN)

	migratorDSN := roleDSN(t, ctx, pgc, "lookupkb_migrator", migratorPassword)

	// --- up ---
	require.NoError(t, appdb.RunMigrations(ctx, migratorDSN, "up"), "goose up must apply cleanly")
	assertTablesExist(t, ctx, superDSN)
	assertDocumentACLDropped(t, ctx, superDSN, true)
	assertAppRoleCannotDDL(t, ctx, pgc)

	// --- 00012 down then up: published_md is backfilled from the working
	// copy for published rows only ---
	require.NoError(t, appdb.RunMigrations(ctx, migratorDSN, "down"), "00013 down must apply")
	require.NoError(t, appdb.RunMigrations(ctx, migratorDSN, "down"), "00012 down must apply")
	assertPublishedMDBackfill(t, ctx, superDSN, func() {
		require.NoError(t, appdb.RunMigrations(ctx, migratorDSN, "up"), "00012 re-applies")
	})

	// --- 00011 down then up: the document ACL comes back empty, and is
	// usable by the app role, then goes again ---
	require.NoError(t, appdb.RunMigrations(ctx, migratorDSN, "down"), "00013 down must apply")
	require.NoError(t, appdb.RunMigrations(ctx, migratorDSN, "down"), "00012 down must apply")
	require.NoError(t, appdb.RunMigrations(ctx, migratorDSN, "down"), "00011 down must apply")
	assertDocumentACLDropped(t, ctx, superDSN, false)
	appConn := open(t, roleDSN(t, ctx, pgc, "lookupkb_app", appPassword))
	_, err = appConn.ExecContext(ctx, "SELECT count(*) FROM document_acl")
	require.NoError(t, err, "the recreated document_acl is granted to the app role")
	_ = appConn.Close()
	require.NoError(t, appdb.RunMigrations(ctx, migratorDSN, "up"), "00011 re-applies")
	assertDocumentACLDropped(t, ctx, superDSN, true)

	// --- down (full reset) then up again: proves every migration reverses ---
	require.NoError(t, appdb.RunMigrations(ctx, migratorDSN, "reset"), "goose down/reset must reverse cleanly")
	assertNoAppTables(t, ctx, superDSN)
	require.NoError(t, appdb.RunMigrations(ctx, migratorDSN, "up"), "goose up must re-apply after a full down")
}

func bootstrapRoles(t *testing.T, ctx context.Context, superDSN string) {
	t.Helper()
	conn := open(t, superDSN)
	defer func() { _ = conn.Close() }()

	stmts := []string{
		fmt.Sprintf("CREATE ROLE lookupkb_migrator LOGIN PASSWORD '%s'", migratorPassword),
		fmt.Sprintf("CREATE ROLE lookupkb_app LOGIN PASSWORD '%s'", appPassword),
		"GRANT CREATE, USAGE ON SCHEMA public TO lookupkb_migrator",
		// Lets the non-superuser migrator install the trusted citext/pgcrypto
		// extensions (mirrors 20-lookupkb-roles.sh).
		"GRANT CREATE ON DATABASE lookupkb TO lookupkb_migrator",
		"GRANT USAGE ON SCHEMA public TO lookupkb_app",
	}
	for _, s := range stmts {
		_, err := conn.ExecContext(ctx, s)
		require.NoError(t, err, s)
	}
}

func assertTablesExist(t *testing.T, ctx context.Context, dsn string) {
	t.Helper()
	conn := open(t, dsn)
	defer func() { _ = conn.Close() }()

	want := []string{
		"users", "groups", "group_members", "documents", "tags",
		"document_tags", "attachments", "audit_log",
		"settings", "api_tokens", "index_outbox", "group_tag_grants",
	}
	for _, tbl := range want {
		var exists bool
		err := conn.QueryRowContext(ctx,
			"SELECT to_regclass('public.'||$1) IS NOT NULL", tbl).Scan(&exists)
		require.NoError(t, err)
		require.True(t, exists, "table %q should exist after up", tbl)
	}
}

// assertDocumentACLDropped checks the schema cleanup: document_acl,
// documents.access and the document_access enum are gone (dropped=true) or back
// (dropped=false, after 00011 down).
func assertDocumentACLDropped(t *testing.T, ctx context.Context, dsn string, dropped bool) {
	t.Helper()
	conn := open(t, dsn)
	defer func() { _ = conn.Close() }()

	var table, column, enum bool
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT to_regclass('public.document_acl') IS NOT NULL").Scan(&table))
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_schema='public' AND table_name='documents' AND column_name='access')`).Scan(&column))
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT to_regtype('public.document_access') IS NOT NULL").Scan(&enum))
	require.Equal(t, !dropped, table, "document_acl")
	require.Equal(t, !dropped, column, "documents.access")
	require.Equal(t, !dropped, enum, "document_access enum")
}

// assertPublishedMDBackfill seeds documents with 00012 rolled back, runs up, and
// checks the best-effort backfill: published rows take their working copy (a
// NULL source becomes the empty string), drafts and unpublished rows stay NULL.
// The seed rows are removed afterwards.
func assertPublishedMDBackfill(t *testing.T, ctx context.Context, dsn string, up func()) {
	t.Helper()
	conn := open(t, dsn)
	defer func() { _ = conn.Close() }()

	var uid string
	require.NoError(t, conn.QueryRowContext(ctx,
		`INSERT INTO users (email, password_hash) VALUES ('backfill@example.com', 'x') RETURNING id`).Scan(&uid))
	_, err := conn.ExecContext(ctx, `INSERT INTO documents (slug, author_id, status, source_md, published_at) VALUES
		('bf-pub', $1, 'published', '# published', now()),
		('bf-pub-null', $1, 'published', NULL, now()),
		('bf-draft', $1, 'draft', '# draft', NULL),
		('bf-unpub', $1, 'unpublished', '# unpub', now())`, uid)
	require.NoError(t, err)

	up()

	got := map[string]*string{}
	rows, err := conn.QueryContext(ctx, `SELECT slug, published_md FROM documents WHERE slug LIKE 'bf-%'`)
	require.NoError(t, err)
	for rows.Next() {
		var slug string
		var md *string
		require.NoError(t, rows.Scan(&slug, &md))
		got[slug] = md
	}
	require.NoError(t, rows.Err())
	_ = rows.Close()
	require.NotNil(t, got["bf-pub"])
	require.Equal(t, "# published", *got["bf-pub"])
	require.NotNil(t, got["bf-pub-null"])
	require.Equal(t, "", *got["bf-pub-null"])
	require.Nil(t, got["bf-draft"], "drafts have no published source")
	require.Nil(t, got["bf-unpub"], "unpublished rows have no published source")

	_, err = conn.ExecContext(ctx, `DELETE FROM documents WHERE author_id = $1`, uid)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, uid)
	require.NoError(t, err)
}

func assertNoAppTables(t *testing.T, ctx context.Context, dsn string) {
	t.Helper()
	conn := open(t, dsn)
	defer func() { _ = conn.Close() }()

	var count int
	err := conn.QueryRowContext(ctx,
		"SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name='documents'").Scan(&count)
	require.NoError(t, err)
	require.Zero(t, count, "documents should be gone after a full down")
}

// assertAppRoleCannotDDL connects as lookupkb_app and confirms DML works but DDL
// is denied — the ticket AC verified by an observed permission error.
func assertAppRoleCannotDDL(t *testing.T, ctx context.Context, pgc *postgres.PostgresContainer) {
	t.Helper()
	appDSN := roleDSN(t, ctx, pgc, "lookupkb_app", appPassword)
	conn := open(t, appDSN)
	defer func() { _ = conn.Close() }()

	// DML is allowed (INSERT a user, then read it back).
	_, err := conn.ExecContext(ctx,
		"INSERT INTO users (email, password_hash) VALUES ('a@example.com', 'x')")
	require.NoError(t, err, "app role should be able to INSERT")

	// the migrator-created group_tag_grants table is usable by the app
	// role (explicit GRANT in 00009), and users.is_super_admin is writable.
	_, err = conn.ExecContext(ctx, "SELECT count(*) FROM group_tag_grants")
	require.NoError(t, err, "app role should be able to read group_tag_grants")
	_, err = conn.ExecContext(ctx, `
		WITH g AS (INSERT INTO groups (name) VALUES ('ops') RETURNING id),
		     t AS (INSERT INTO tags (slug, label) VALUES ('secret', 'secret') RETURNING id)
		INSERT INTO group_tag_grants (group_id, tag_id) SELECT g.id, t.id FROM g, t`)
	require.NoError(t, err, "app role should be able to INSERT into group_tag_grants")
	_, err = conn.ExecContext(ctx, "UPDATE users SET is_super_admin = true WHERE email = 'a@example.com'")
	require.NoError(t, err, "app role should be able to set is_super_admin")

	// DDL must be refused.
	_, err = conn.ExecContext(ctx, "DROP TABLE users")
	require.Error(t, err, "app role must NOT be able to DROP a table")

	_, err = conn.ExecContext(ctx, "ALTER TABLE documents ADD COLUMN hacked text")
	require.Error(t, err, "app role must NOT be able to ALTER a table")
}

// roleDSN rebuilds the container DSN for a specific role/password.
func roleDSN(t *testing.T, ctx context.Context, pgc *postgres.PostgresContainer, role, password string) string {
	t.Helper()
	host, err := pgc.Host(ctx)
	require.NoError(t, err)
	port, err := pgc.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)
	return fmt.Sprintf("postgres://%s:%s@%s:%s/lookupkb?sslmode=disable",
		role, password, host, port.Port())
}

func open(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	conn, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	require.NoError(t, conn.PingContext(context.Background()))
	return conn
}
