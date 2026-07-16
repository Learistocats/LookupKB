// Package server wires the Fiber HTTP application.
//
// The handler layer is deliberately thin: routes delegate to small functions
// so the framework stays swappable (the tech-stack doc keeps Echo as a
// fallback). Only liveness/readiness live here today; feature routes arrive in
// later tickets.
package server

import (
	"log/slog"

	"github.com/Learistocats/LookupKB/api/internal/config"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

// ReadinessCheck reports whether a dependency is ready to serve traffic.
// Later tickets register real checks (Postgres, Meilisearch, Kroki, Garage,
// the OTel collector). The map returned by /readyz is keyed by check name.
type ReadinessCheck func() error

// Server holds the Fiber app and its dependencies.
type Server struct {
	app             *fiber.App
	log             *slog.Logger
	cfg             config.Config
	version         string
	readinessChecks map[string]ReadinessCheck
}

// New constructs a Server with base middleware and health routes registered.
func New(cfg config.Config, log *slog.Logger, version string) *Server {
	app := fiber.New(fiber.Config{
		AppName:               "lookupkb-api",
		DisableStartupMessage: true,
		// Behind Caddy: trust proxy headers only from the reverse proxy.
		ProxyHeader: fiber.HeaderXForwardedFor,
	})

	s := &Server{
		app:             app,
		log:             log,
		cfg:             cfg,
		version:         version,
		readinessChecks: map[string]ReadinessCheck{},
	}

	app.Use(requestid.New())
	app.Use(recover.New())

	s.registerRoutes()
	return s
}

// App exposes the underlying Fiber app (used by tests and main).
func (s *Server) App() *fiber.App {
	return s.app
}

// RegisterReadinessCheck adds a named dependency check consulted by /readyz.
func (s *Server) RegisterReadinessCheck(name string, check ReadinessCheck) {
	s.readinessChecks[name] = check
}

// Listen starts serving on the configured address.
func (s *Server) Listen() error {
	return s.app.Listen(s.cfg.Addr())
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown() error {
	return s.app.ShutdownWithTimeout(s.cfg.ShutdownTimeout)
}

func (s *Server) registerRoutes() {
	s.app.Get("/healthz", s.handleHealthz)
	s.app.Get("/readyz", s.handleReadyz)
}

// handleHealthz is the liveness probe: the process is up. It never touches
// dependencies so it stays trivially fast and always 200 while running.
func (s *Server) handleHealthz(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"status":  "ok",
		"service": "lookupkb-api",
		"version": s.version,
	})
}

// handleReadyz is the readiness probe: every registered dependency check must
// pass. With no checks registered (M0 skeleton) it reports ready.
func (s *Server) handleReadyz(c *fiber.Ctx) error {
	checks := make(map[string]string, len(s.readinessChecks))
	ready := true
	for name, check := range s.readinessChecks {
		if err := check(); err != nil {
			ready = false
			checks[name] = err.Error()
			continue
		}
		checks[name] = "ok"
	}

	status := fiber.StatusOK
	statusText := "ready"
	if !ready {
		status = fiber.StatusServiceUnavailable
		statusText = "not ready"
	}

	return c.Status(status).JSON(fiber.Map{
		"status": statusText,
		"checks": checks,
	})
}
