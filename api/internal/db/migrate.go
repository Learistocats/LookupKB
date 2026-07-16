package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver for goose
	"github.com/pressly/goose/v3"
)

// migrationsFS embeds the goose SQL migrations so `api migrate` needs no files
// on disk — it runs from the distroless image (which has no shell) exactly as
// the `api healthcheck` self-probe does.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations exposes the embedded migration files for tooling (e.g. the
// integration test's testcontainers run) that drives goose directly.
var Migrations = migrationsFS

const migrationsDir = "migrations"

// RunMigrations applies (up), reverses (down), or reports (status) migrations
// against dsn, which must be the migration-role connection string (owns DDL).
func RunMigrations(ctx context.Context, dsn, direction string) error {
	if dsn == "" {
		return fmt.Errorf("no migration database URL configured")
	}

	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open migration db: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()

	return RunMigrationsDB(ctx, sqlDB, direction)
}

// RunMigrationsDB runs migrations against an already-open *sql.DB. It is shared
// by RunMigrations and by tests that own the connection.
func RunMigrationsDB(ctx context.Context, sqlDB *sql.DB, direction string) error {
	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}

	switch direction {
	case "up":
		return goose.UpContext(ctx, sqlDB, migrationsDir)
	case "down":
		return goose.DownContext(ctx, sqlDB, migrationsDir)
	case "reset":
		return goose.ResetContext(ctx, sqlDB, migrationsDir)
	case "status":
		return goose.StatusContext(ctx, sqlDB, migrationsDir)
	default:
		return fmt.Errorf("unknown migration direction %q (want up|down|reset|status)", direction)
	}
}
