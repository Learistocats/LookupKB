// Package db owns the PostgreSQL connection pool, embedded schema migrations,
// and (via the generated dbgen subpackage) type-safe queries. Postgres is
// LookupKB's authoritative store (01-architecture.md §2).
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/Learistocats/LookupKB/api/internal/config"
	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool opens a pgx connection pool using the runtime (application-role) DSN
// and verifies connectivity before returning. Pool sizing and timeouts come
// from config so nothing but the environment tunes a running container.
func NewPool(ctx context.Context, cfg config.Config) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	poolCfg.MaxConns = cfg.DBMaxConns
	poolCfg.MinConns = cfg.DBMinConns
	poolCfg.MaxConnLifetime = cfg.DBMaxConnLifetime
	poolCfg.MaxConnIdleTime = cfg.DBMaxConnIdleTime
	poolCfg.ConnConfig.ConnectTimeout = cfg.DBConnectTimeout

	// Trace every query as a child span of the request's span (via the global
	// TracerProvider set up by internal/telemetry), so DB calls are attributable
	// per request. A no-op tracer when telemetry is disabled — zero overhead.
	poolCfg.ConnConfig.Tracer = otelpgx.NewTracer()

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}

// ReadinessCheck returns a probe (for the server's /readyz) that reports the
// pool can reach Postgres within a short bound.
func ReadinessCheck(pool *pgxpool.Pool) func() error {
	return func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			return fmt.Errorf("postgres unreachable: %w", err)
		}
		return nil
	}
}
