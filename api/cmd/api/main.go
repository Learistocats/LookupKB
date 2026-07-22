// Command api is the LookupKB backend HTTP server.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/Learistocats/LookupKB/api/internal/config"
	"github.com/Learistocats/LookupKB/api/internal/db"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/Learistocats/LookupKB/api/internal/health"
	"github.com/Learistocats/LookupKB/api/internal/logging"
	"github.com/Learistocats/LookupKB/api/internal/metrics"
	"github.com/Learistocats/LookupKB/api/internal/server"
	"github.com/Learistocats/LookupKB/api/internal/tags"
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

	// `api healthcheck` / `api readycheck` are self-probes used as the container
	// HEALTHCHECK: the distroless image has no shell or curl, so the binary probes
	// itself. healthcheck hits /healthz (liveness, restart semantics); readycheck
	// hits /readyz so a compose healthcheck can reflect real dependency readiness.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(selfProbe(cfg, "/healthz"))
	}
	if len(os.Args) > 1 && os.Args[1] == "readycheck" {
		os.Exit(selfProbe(cfg, "/readyz"))
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

// selfProbe performs an in-container GET against the given local path and returns
// a process exit code (0 healthy, 1 unhealthy) for Docker's HEALTHCHECK. Used by
// both the `healthcheck` (/healthz) and `readycheck` (/readyz) subcommands.
func selfProbe(cfg config.Config, path string) int {
	client := &http.Client{Timeout: 3 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d%s", cfg.Port, path)
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
		verifier, err := discoverOIDC(cfg, log)
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

	// Wire the walking-skeleton draft routes when Postgres is configured. Without
	// a pool the routes are omitted (the skeleton still boots for bare `go run`).
	if pool != nil {
		opts = append(opts, server.WithDocuments(dbgen.New(pool)))
		opts = append(opts, server.WithTags(tags.New(pool)))
	}

	srv := server.New(cfg, log, version, opts...)
	if pool != nil {
		srv.RegisterReadinessCheck("postgres", db.ReadinessCheck(pool))
	}
	registerDependencyChecks(cfg, srv, log)

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

// discoverOIDC resolves the OIDC verifier, tolerating an Authentik that is not
// ready yet at startup. Authentik boots slower than the API (fresh DB migrations
// + blueprint application), and its front proxy returns 502 "authentik starting"
// until the backend is up. The API is in the core stack while Authentik is
// profile-gated, so this ordering can't be a compose depends_on — instead we
// retry discovery with a short backoff until Authentik answers or the deadline
// elapses, rather than exiting and relying on a container crash-loop.
func discoverOIDC(cfg config.Config, log *slog.Logger) (*auth.Verifier, error) {
	const (
		perAttempt = 10 * time.Second
		interval   = 3 * time.Second
		maxWait    = 90 * time.Second
	)
	deadline := time.Now().Add(maxWait)
	var lastErr error
	for attempt := 1; ; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), perAttempt)
		verifier, err := auth.New(ctx, cfg)
		cancel()
		if err == nil {
			if attempt > 1 {
				log.Info("oidc discovery succeeded after retry", "attempts", attempt)
			}
			return verifier, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("oidc discovery failed after %s: %w", maxWait, lastErr)
		}
		log.Warn("oidc discovery not ready, retrying", "attempt", attempt, "error", err.Error())
		time.Sleep(interval)
	}
}

// registerDependencyChecks wires the /readyz probes for the downstream services
// (AC-11.1). Each is registered only when its URL is configured, so the skeleton
// stays ready without the dependency (mirroring the empty-DATABASE_URL pattern).
// Meili/Kroki/Garage gate readiness; the OTel collector is best-effort so local
// dev without observability is never "not ready". Each probe is bounded by
// cfg.ReadinessTimeout and cached for cfg.ReadinessCacheTTL.
func registerDependencyChecks(cfg config.Config, srv *server.Server, log *slog.Logger) {
	cache := cfg.ReadinessCacheTTL
	timeout := cfg.ReadinessTimeout

	gating := []struct{ name, url string }{
		{"meilisearch", cfg.MeiliHealthURL()},
		{"kroki", cfg.KrokiHealthURL()},
		{"garage", cfg.GarageHealthURL()},
	}
	for _, dep := range gating {
		if dep.url == "" {
			log.Warn("readiness check disabled (no URL configured)", "check", dep.name)
			continue
		}
		srv.RegisterReadinessCheck(dep.name, health.Cached(cache, health.HTTPCheck(dep.name, dep.url, timeout)))
	}

	if url := cfg.CollectorHealthURL(); url != "" {
		srv.RegisterInformationalCheck("otel_collector", health.Cached(cache, health.HTTPCheck("otel_collector", url, timeout)))
	}
}
