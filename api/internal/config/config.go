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

// Config holds all runtime configuration for the API, flat and env-driven.
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

	// MeiliURL is the Meilisearch base URL; empty disables search and its
	// readiness check. MeiliKey is the API key for indexing/search (server-side only).
	MeiliURL string `env:"LOOKUPKB_MEILI_URL"`
	MeiliKey string `env:"LOOKUPKB_MEILI_KEY"`

	// ReadinessTimeout bounds each /readyz dependency probe.
	ReadinessTimeout time.Duration `env:"LOOKUPKB_READINESS_TIMEOUT" envDefault:"2s"`

	// ReadinessCacheTTL memoizes each probe's result; 0 disables caching.
	ReadinessCacheTTL time.Duration `env:"LOOKUPKB_READINESS_CACHE_TTL" envDefault:"1s"`

	// MarkdownMaxBytes caps the Markdown source accepted at publish; larger
	// source is rejected before rendering (render.ErrSourceTooLarge).
	MarkdownMaxBytes int `env:"LOOKUPKB_MARKDOWN_MAX_BYTES" envDefault:"1048576"`

	// MCPAllowedOrigins are the browser origins allowed to call /mcp. Any other
	// Origin header is refused (DNS-rebinding defence); native clients send none.
	MCPAllowedOrigins []string `env:"LOOKUPKB_MCP_ALLOWED_ORIGINS" envSeparator:","`
}

// MeiliHealthURL returns the Meilisearch health endpoint, or "" when unconfigured.
func (c Config) MeiliHealthURL() string { return healthURL(c.MeiliURL, "/health") }

// healthURL joins base and path; an empty base stays empty (check disabled).
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
