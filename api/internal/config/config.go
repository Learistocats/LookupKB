// Package config loads LookupKB API configuration from the environment
// following 12-factor conventions. Every field maps to a documented variable
// in the repository-root .env.example.
package config

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
)

// Config holds all runtime configuration for the API.
//
// Only variables needed by the M0 scaffolding are wired today; later tickets
// (OIDC, Postgres, Meilisearch, Garage, OTel) add their own fields. Keep the
// struct flat and env-driven so nothing but the environment configures a
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

	// OIDCIssuer is the external issuer URL that must match the `iss` claim in
	// tokens (e.g. https://kb.example.com/application/o/lookupkb/). Empty
	// disables token verification so the skeleton/tests boot without Authentik
	// (mirrors the empty-DatabaseURL behaviour). See 06-security.md §5.
	OIDCIssuer string `env:"LOOKUPKB_OIDC_ISSUER"`

	// OIDCIssuerInternal is the URL the API reaches for OIDC discovery and JWKS
	// (e.g. http://authentik-server:9000/application/o/lookupkb/). Behind a
	// reverse proxy this differs from the external issuer: JWKS is fetched here
	// but the `iss` claim is still validated against OIDCIssuer. Empty falls
	// back to OIDCIssuer (no split-horizon).
	OIDCIssuerInternal string `env:"LOOKUPKB_OIDC_ISSUER_INTERNAL"`

	// OIDCAudience is the expected `aud` claim (the LookupKB OAuth2 client id).
	// Validated explicitly alongside issuer/signature/expiry — a JWKS-only check
	// would let tokens minted for other Authentik apps be replayed here.
	OIDCAudience string `env:"LOOKUPKB_OIDC_AUDIENCE"`

	// OTelExporterEndpoint is the OTLP endpoint the OpenTelemetry SDK exports
	// traces/metrics/logs to (the otel-collector, e.g.
	// http://otel-collector:4317). It follows the OTel spec's standard variable
	// so `OTEL_EXPORTER_OTLP_*` overrides work unchanged. Empty disables the SDK
	// (no-op providers) so the skeleton/tests boot without a collector — the same
	// pattern as an empty DatabaseURL or OIDCIssuer. An `http://` scheme selects
	// the insecure (plaintext) gRPC transport automatically.
	OTelExporterEndpoint string `env:"OTEL_EXPORTER_OTLP_ENDPOINT"`
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

// AuthEnabled reports whether OIDC token verification is configured. When the
// issuer is empty the API boots without protected routes (dev/tests), the same
// pattern as an empty DatabaseURL disabling the pool.
func (c Config) AuthEnabled() bool {
	return c.OIDCIssuer != ""
}

// OTelEnabled reports whether OpenTelemetry export is configured. When the OTLP
// endpoint is empty the SDK is not initialised (no-op providers) so the API
// boots without a collector — the same pattern as an empty DatabaseURL disabling
// the pool.
func (c Config) OTelEnabled() bool {
	return c.OTelExporterEndpoint != ""
}

// OIDCDiscoveryURL returns the URL used for OIDC discovery / JWKS fetch,
// preferring the internal URL when set (reverse-proxy split-horizon) and
// otherwise the external issuer.
func (c Config) OIDCDiscoveryURL() string {
	if c.OIDCIssuerInternal != "" {
		return c.OIDCIssuerInternal
	}
	return c.OIDCIssuer
}

// Load parses configuration from the process environment.
func Load() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config from env: %w", err)
	}
	return cfg, nil
}
