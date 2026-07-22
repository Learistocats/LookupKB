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

	// Downstream dependency base URLs used only by the /readyz probes
	// (01-architecture.md §6, AC-11.1). Each is the service's base URL; the
	// health path is appended by the *HealthURL helpers below. Following the
	// "empty disables it" convention, an unset URL skips that check so the
	// skeleton/tests stay ready without the dependency running.
	MeiliURL       string `env:"LOOKUPKB_MEILI_URL"`
	KrokiURL       string `env:"LOOKUPKB_KROKI_URL"`
	GarageAdminURL string `env:"LOOKUPKB_GARAGE_ADMIN_URL"`

	// OTelCollectorHealthURL is the collector's health_check extension base URL
	// (e.g. http://otel-collector:13133), distinct from OTelExporterEndpoint
	// (the OTLP :4317 target). Probed *best-effort* by /readyz: a down collector
	// is reported but never makes the API "not ready" (AC-11.1 gates only on
	// Postgres/Meili/Kroki/Garage — local dev without observability must stay
	// ready). Empty skips the check.
	OTelCollectorHealthURL string `env:"LOOKUPKB_OTEL_COLLECTOR_HEALTH_URL"`

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

// CollectorHealthURL returns the OTel collector health_check endpoint (served at
// the extension root), or "" when unconfigured.
func (c Config) CollectorHealthURL() string { return healthURL(c.OTelCollectorHealthURL, "/") }

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
