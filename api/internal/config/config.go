// Package config loads LookupKB API configuration from the environment
// following 12-factor conventions. Every field maps to a documented variable
// in the repository-root .env.example.
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

// Config holds all runtime configuration for the API.
//
// Keep the struct flat and env-driven so nothing but the environment configures a
// running container.
type Config struct {
	// Env is the deployment environment: "development", "staging" or
	// "production". It tunes logging defaults.
	Env string `env:"LOOKUPKB_ENV" envDefault:"development"`

	// Host and Port are the API listen address.
	Host string `env:"LOOKUPKB_API_HOST" envDefault:"0.0.0.0"`
	Port int    `env:"LOOKUPKB_API_PORT" envDefault:"8080"`

	// LogLevel is one of debug|info|warn|error.
	LogLevel string `env:"LOOKUPKB_LOG_LEVEL" envDefault:"info"`

	// LogFormat is "json" (production default) or "text" (dev-friendly).
	LogFormat string `env:"LOOKUPKB_LOG_FORMAT" envDefault:"json"`

	// ShutdownTimeout bounds graceful shutdown.
	ShutdownTimeout time.Duration `env:"LOOKUPKB_SHUTDOWN_TIMEOUT" envDefault:"10s"`

	// DatabaseURL is the pgx connection string the runtime uses, connecting as
	// the least-privilege application role (DML only). Empty disables the pool
	// so the skeleton/tests boot without a database.
	DatabaseURL string `env:"LOOKUPKB_DATABASE_URL"`

	// MigrateDatabaseURL is the connection string used by `api migrate`,
	// connecting as the migration role that owns DDL. Empty falls back to
	// DatabaseURL (dev convenience).
	MigrateDatabaseURL string `env:"LOOKUPKB_MIGRATE_DATABASE_URL"`

	// Connection-pool tuning (pgxpool). Defaults are sane for a single-host
	// deployment; override via env under load.
	DBMaxConns        int32         `env:"LOOKUPKB_DB_MAX_CONNS" envDefault:"10"`
	DBMinConns        int32         `env:"LOOKUPKB_DB_MIN_CONNS" envDefault:"0"`
	DBMaxConnLifetime time.Duration `env:"LOOKUPKB_DB_MAX_CONN_LIFETIME" envDefault:"1h"`
	DBMaxConnIdleTime time.Duration `env:"LOOKUPKB_DB_MAX_CONN_IDLE_TIME" envDefault:"30m"`
	DBConnectTimeout  time.Duration `env:"LOOKUPKB_DB_CONNECT_TIMEOUT" envDefault:"5s"`

	// AuthSecret is the HS256 secret the API signs and verifies its own access
	// tokens with (≥32 bytes). Empty disables authentication so the skeleton/tests
	// boot without it — no protected routes are registered (mirrors the
	// empty-DatabaseURL behaviour).
	AuthSecret string `env:"LOOKUPKB_AUTH_SECRET"`

	// AuthTokenTTL bounds each access token's lifetime. There is no refresh: the
	// user logs in again once it expires.
	AuthTokenTTL time.Duration `env:"LOOKUPKB_AUTH_TOKEN_TTL" envDefault:"168h"`

	// ValkeyURL (redis://host:6379/0) backs the login/signup rate limiter so limits
	// survive restarts. Empty falls back to an in-process limiter.
	ValkeyURL string `env:"LOOKUPKB_VALKEY_URL"`

	// Downstream dependency base URLs used only by the /readyz probes
	// (01-architecture.md §6, AC-11.1). Each is the service's base URL; the
	// health path is appended by the *HealthURL helpers below. Following the
	// "empty disables it" convention, an unset URL skips that check so the
	// skeleton/tests stay ready without the dependency running.
	MeiliURL       string `env:"LOOKUPKB_MEILI_URL"`
	KrokiURL       string `env:"LOOKUPKB_KROKI_URL"`
	GarageAdminURL string `env:"LOOKUPKB_GARAGE_ADMIN_URL"`

	// ReadinessTimeout bounds each /readyz dependency probe so an unreachable
	// dependency can never hang the readiness endpoint (AC-11.1).
	ReadinessTimeout time.Duration `env:"LOOKUPKB_READINESS_TIMEOUT" envDefault:"2s"`

	// ReadinessCacheTTL memoizes each probe's result for this window so frequent
	// orchestrator polling doesn't hammer the dependencies. 0 disables caching.
	ReadinessCacheTTL time.Duration `env:"LOOKUPKB_READINESS_CACHE_TTL" envDefault:"1s"`

	// MarkdownMaxBytes caps the Markdown source size accepted at publish, bounding
	// server-side render cost (06-security.md §6 DoS row). Source exceeding it is
	// rejected before rendering (render.ErrSourceTooLarge), never truncated. The
	// limit is admin-tunable; the default (1 MiB) is generous for prose documents.
	MarkdownMaxBytes int `env:"LOOKUPKB_MARKDOWN_MAX_BYTES" envDefault:"1048576"`
}

// MeiliHealthURL returns the Meilisearch health endpoint, or "" when unconfigured.
func (c Config) MeiliHealthURL() string { return healthURL(c.MeiliURL, "/health") }

// KrokiHealthURL returns the Kroki health endpoint, or "" when unconfigured.
func (c Config) KrokiHealthURL() string { return healthURL(c.KrokiURL, "/health") }

// GarageHealthURL returns the Garage admin health endpoint, or "" when unconfigured.
func (c Config) GarageHealthURL() string { return healthURL(c.GarageAdminURL, "/health") }

// healthURL joins a configured base with its health path, preserving the
// "empty base disables the check" convention.
func healthURL(base, path string) string {
	if base == "" {
		return ""
	}
	return strings.TrimRight(base, "/") + path
}

// Addr returns the host:port listen address.
func (c Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// MigrateDSN returns the connection string for running migrations, falling back
// to DatabaseURL when no dedicated migration DSN is configured (dev convenience).
func (c Config) MigrateDSN() string {
	if c.MigrateDatabaseURL != "" {
		return c.MigrateDatabaseURL
	}
	return c.DatabaseURL
}

// IsProduction reports whether the API runs in a production-like environment.
func (c Config) IsProduction() bool {
	return c.Env == "production"
}

// AuthEnabled reports whether token authentication is configured. When the
// secret is empty the API boots without protected routes (dev/tests), the same
// pattern as an empty DatabaseURL disabling the pool.
func (c Config) AuthEnabled() bool {
	return c.AuthSecret != ""
}

// Load parses configuration from the process environment.
func Load() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config from env: %w", err)
	}
	return cfg, nil
}
