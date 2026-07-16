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
}

// Addr returns the host:port listen address.
func (c Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// IsProduction reports whether the API runs in a production-like environment.
func (c Config) IsProduction() bool {
	return c.Env == "production"
}

// Load parses configuration from the process environment.
func Load() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config from env: %w", err)
	}
	return cfg, nil
}
