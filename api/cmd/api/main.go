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

	"github.com/Learistocats/LookupKB/api/internal/config"
	"github.com/Learistocats/LookupKB/api/internal/db"
	"github.com/Learistocats/LookupKB/api/internal/logging"
	"github.com/Learistocats/LookupKB/api/internal/server"
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

	log := logging.New(cfg.LogFormat, cfg.LogLevel, version)

	if err := run(cfg, log); err != nil {
		log.Error("server exited with error", "error", err)
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

	log := logging.New(cfg.LogFormat, cfg.LogLevel, version)
	ctx := context.Background()

	if err := db.RunMigrations(ctx, cfg.MigrateDSN(), direction); err != nil {
		log.Error("migration failed", "direction", direction, "error", err)
		return 1
	}
	log.Info("migrations applied", "direction", direction)
	return 0
}

func run(cfg config.Config, log *slog.Logger) error {
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

	srv := server.New(cfg, log, version)
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
