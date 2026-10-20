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
	"github.com/Learistocats/LookupKB/api/internal/publish"
	"github.com/Learistocats/LookupKB/api/internal/render"
	"github.com/Learistocats/LookupKB/api/internal/search"
	"github.com/Learistocats/LookupKB/api/internal/server"
	"github.com/Learistocats/LookupKB/api/internal/tags"
	"github.com/Learistocats/LookupKB/api/internal/valkey"
	"github.com/jackc/pgx/v5/pgxpool"
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
		logging.New(cfg.LogFormat, cfg.LogLevel, version).
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

	log := logging.New(cfg.LogFormat, cfg.LogLevel, version)
	ctx := context.Background()

	if err := db.RunMigrations(ctx, cfg.MigrateDSN(), direction); err != nil {
		log.Error("migration failed", "direction", direction, "error", err)
		return 1
	}
	log.Info("migrations applied", "direction", direction)
	return 0
}

func run(cfg config.Config, version string) error {
	log := logging.New(cfg.LogFormat, cfg.LogLevel, version)

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

	// Configure token authentication when a secret is set. An empty secret keeps
	// the API open (no protected routes) so bare `go run` and tests boot without it,
	// mirroring the empty-DATABASE_URL behaviour.
	var opts []server.Option
	if cfg.AuthEnabled() {
		tokens, err := auth.NewTokens(cfg.AuthSecret, cfg.AuthTokenTTL)
		if err != nil {
			return err
		}
		var groups auth.GroupLister
		var pats auth.PATVerifier
		if pool != nil {
			groups = dbgen.New(pool)
			// Signup/login need the users table, so they exist only with a database.
			opts = append(opts, server.WithAccounts(dbgen.New(pool), tokens))
			// Personal Access Tokens: verification + management need api_tokens.
			// Without a pool the verifier stays nil and every PAT is rejected.
			pats = auth.NewDBPATStore(dbgen.New(pool))
			opts = append(opts, server.WithTokens(dbgen.New(pool)))
		} else {
			log.Warn("auth enabled without a database; signup/login, group lookup and PATs are disabled")
		}
		mw := auth.NewMiddleware(tokens, groups, log)
		if pats != nil {
			mw.WithPATs(pats)
		}
		opts = append(opts, server.WithAuth(mw))
		log.Info("token authentication enabled", "token_ttl", cfg.AuthTokenTTL.String())
	} else {
		log.Warn("LOOKUPKB_AUTH_SECRET is empty; protected routes are disabled")
	}

	// Share login/signup rate-limit counters through Valkey when configured; the
	// limiter otherwise counts in process memory (fine for a single instance).
	var kv *valkey.Storage
	if cfg.ValkeyURL != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		store, err := valkey.New(ctx, cfg.ValkeyURL)
		if err != nil {
			return err
		}
		kv = store
		defer func() { _ = kv.Close() }()
		opts = append(opts, server.WithLimiterStorage(kv))
		log.Info("valkey rate-limit storage ready")
	}

	// Wire the document routes when Postgres is configured. Without a pool the
	// routes are omitted (the skeleton still boots for bare `go run`).
	if pool != nil {
		opts = append(opts, server.WithDocuments(dbgen.New(pool)))
		opts = append(opts, server.WithTags(tags.New(pool)))
		// One process-wide renderer (concurrency-safe) drives the publish gate's
		// server-side render+sanitize; the publish service composes it with the
		// tag/audit/outbox writes into one transaction.
		renderer := render.New()
		opts = append(opts, server.WithPublish(publish.NewService(pool, renderer, log)))
	}

	// Search needs both Postgres (the outbox + projections) and Meilisearch. The
	// worker drains index_outbox into the index for the life of the process.
	workerCtx, stopWorker := context.WithCancel(context.Background())
	defer stopWorker()
	if pool != nil && cfg.MeiliURL != "" {
		meili := search.NewClient(cfg.MeiliURL, cfg.MeiliKey)
		opts = append(opts, server.WithSearch(search.NewService(meili)))
		go search.NewWorker(dbgen.New(pool), meili, log).Run(workerCtx)
	} else {
		log.Warn("search disabled (needs LOOKUPKB_DATABASE_URL and LOOKUPKB_MEILI_URL)")
	}

	srv := server.New(cfg, log, version, opts...)
	if pool != nil {
		srv.RegisterReadinessCheck("postgres", db.ReadinessCheck(pool))
	}
	if kv != nil {
		srv.RegisterReadinessCheck("valkey", kv.Ping)
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

// registerDependencyChecks wires the /readyz probes for the downstream services
// (AC-11.1). Each is registered only when its URL is configured, so the skeleton
// stays ready without the dependency (mirroring the empty-DATABASE_URL pattern).
// Meili/Kroki/Garage gate readiness. Each probe is bounded by
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
}
