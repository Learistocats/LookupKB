// Command api is the LookupKB backend HTTP server.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/Learistocats/LookupKB/api/internal/config"
	"github.com/Learistocats/LookupKB/api/internal/db"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/Learistocats/LookupKB/api/internal/logging"
	"github.com/Learistocats/LookupKB/api/internal/metrics"
	"github.com/Learistocats/LookupKB/api/internal/server"
	"github.com/Learistocats/LookupKB/api/internal/telemetry"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
)

// version is stamped at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	cfg, err := config.Load()
	if err != nil {
		// No logger yet; fail loudly and exit non-zero.
		panic(err)
	}

	// `api healthcheck` is a self-probe used as the container HEALTHCHECK: the
	// distroless image has no shell or curl, so the binary probes itself.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck(cfg))
	}

	// `api migrate up|down|status` runs embedded goose migrations from the same
	// binary (no separate tool in the shell-less image). Used by the compose
	// one-shot migrate service, CI, and `make migrate`.
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		os.Exit(migrate(cfg, version))
	}

	if err := run(cfg, version); err != nil {
		// A stdout-only logger for the fatal path — run owns the OTel-bridged one.
		logging.New(cfg.LogFormat, cfg.LogLevel, version, nil).
			Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

// healthcheck performs an in-container GET /healthz and returns a process exit
// code (0 healthy, 1 unhealthy) for Docker's HEALTHCHECK.
func healthcheck(cfg config.Config) int {
	client := &http.Client{Timeout: 3 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", cfg.Port)
	resp, err := client.Get(url)
	if err != nil {
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

// migrate runs embedded schema migrations and returns a process exit code.
// It logs to stderr via a plain logger since it runs before the server starts.
func migrate(cfg config.Config, version string) int {
	direction := "up"
	if len(os.Args) > 2 {
		direction = os.Args[2]
	}

	log := logging.New(cfg.LogFormat, cfg.LogLevel, version, nil)
	ctx := context.Background()

	if err := db.RunMigrations(ctx, cfg.MigrateDSN(), direction); err != nil {
		log.Error("migration failed", "direction", direction, "error", err)
		return 1
	}
	log.Info("migrations applied", "direction", direction)
	return 0
}

func run(cfg config.Config, version string) error {
	// Initialise the OpenTelemetry SDK first so the logger can bridge to it and
	// every provider is installed before anything is instrumented. A no-op when
	// no OTLP endpoint is configured (boots without a collector).
	initCtx, cancelInit := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelInit()
	loggerProvider, shutdownTelemetry, err := telemetry.Init(initCtx, cfg, version)
	if err != nil {
		return err
	}

	// The process-wide logger: stdout JSON + (when enabled) OTLP logs, every
	// line trace-correlated. Built after telemetry so the bridge is live.
	log := logging.New(cfg.LogFormat, cfg.LogLevel, version, loggerProvider)
	if cfg.OTelEnabled() {
		log.Info("opentelemetry sdk initialised", "endpoint", cfg.OTelExporterEndpoint)
	} else {
		log.Warn("OTEL_EXPORTER_OTLP_ENDPOINT is empty; telemetry export is disabled")
	}
	// Flush traces/metrics/logs on the way out so the last requests before a
	// container stop are not silently dropped.
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTelemetry(flushCtx); err != nil {
			log.Error("telemetry shutdown failed", "error", err)
		}
	}()

	// Register the scaffolded business instruments on the global MeterProvider so
	// later feature tickets can record on them.
	appMetrics, err := metrics.New(otel.GetMeterProvider())
	if err != nil {
		return err
	}

	// Connect to Postgres when configured. Empty DATABASE_URL keeps the M0
	// skeleton bootable without a database (e.g. unit tests, bare `go run`).
	var pool *pgxpool.Pool
	if cfg.DatabaseURL != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		p, err := db.NewPool(ctx, cfg)
		if err != nil {
			return err
		}
		pool = p
		defer pool.Close()
		log.Info("postgres pool ready", "max_conns", cfg.DBMaxConns)
	} else {
		log.Warn("LOOKUPKB_DATABASE_URL is empty; starting without a database")
	}

	// Configure OIDC token verification when an issuer is set. Empty issuer
	// keeps the API open (no protected routes) so the core stack boots without
	// Authentik (--profile auth), mirroring the empty-DATABASE_URL behaviour.
	var opts []server.Option
	if cfg.AuthEnabled() {
		discoverCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		verifier, err := auth.New(discoverCtx, cfg)
		if err != nil {
			return err
		}
		var users auth.UserUpserter
		if pool != nil {
			users = dbgen.New(pool)
		} else {
			log.Warn("auth enabled without a database; user projection upsert is disabled")
		}
		opts = append(opts, server.WithAuth(auth.NewMiddleware(verifier, users, log)))
		log.Info("oidc verification enabled", "issuer", cfg.OIDCIssuer, "audience", cfg.OIDCAudience)
	} else {
		log.Warn("LOOKUPKB_OIDC_ISSUER is empty; protected routes are disabled")
	}

	opts = append(opts, server.WithMetrics(appMetrics))

	srv := server.New(cfg, log, version, opts...)
	if pool != nil {
		srv.RegisterReadinessCheck("postgres", db.ReadinessCheck(pool))
	}

	// Run the listener in a goroutine so we can wait for shutdown signals.
	errCh := make(chan error, 1)
	go func() {
		log.Info("api listening", "addr", cfg.Addr(), "env", cfg.Env)
		errCh <- srv.Listen()
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutdown signal received, draining connections")
		return srv.Shutdown()
	}
}
