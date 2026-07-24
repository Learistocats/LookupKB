// Package server wires the Fiber HTTP application.
//
// The handler layer is deliberately thin: routes delegate to small functions
// so the framework stays swappable (the tech-stack doc keeps Echo as a
// fallback). Only liveness/readiness live here today; feature routes arrive in
// later tickets.
package server

import (
	"log/slog"

	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/Learistocats/LookupKB/api/internal/config"
	"github.com/Learistocats/LookupKB/api/internal/logging"
	"github.com/Learistocats/LookupKB/api/internal/metrics"
	"github.com/gofiber/contrib/otelfiber/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ReadinessCheck reports whether a dependency is ready to serve traffic.
// Later tickets register real checks (Postgres, Meilisearch, Kroki, Garage,
// the OTel collector). The map returned by /readyz is keyed by check name.
type ReadinessCheck func() error

// Server holds the Fiber app and its dependencies.
type Server struct {
	app     *fiber.App
	log     *slog.Logger
	cfg     config.Config
	version string
	// readinessChecks gate /readyz: any failure makes the API "not ready" (503).
	readinessChecks map[string]ReadinessCheck
	// informationalChecks are reported by /readyz but never gate it — used for
	// best-effort dependencies like the OTel collector (AC-11.1 gates only on
	// Postgres/Meili/Kroki/Garage; a down collector must not block local dev).
	informationalChecks map[string]ReadinessCheck
	auth                *auth.Middleware
	metrics             *metrics.Metrics
	documents           DocumentStore
	tags                TagStore
	publish             PublishService
}

// Option configures a Server at construction. Later tickets add their own.
type Option func(*Server)

// WithAuth enables the authenticated route group (/api/v1) guarded by the OIDC
// verification middleware. Omitted when auth is not configured (dev/tests), in
// which case no protected routes are registered.
func WithAuth(mw *auth.Middleware) Option {
	return func(s *Server) { s.auth = mw }
}

// WithMetrics hands the server the scaffolded business instruments so the
// feature routes added by later tickets (search, read, publish) can record on
// them. No route records anything today.
func WithMetrics(m *metrics.Metrics) Option {
	return func(s *Server) { s.metrics = m }
}

// WithDocuments enables the walking-skeleton draft routes (create/save/read).
// The store is the subset of the generated queries those handlers need; omitted
// when no database is configured, in which case the draft routes are not
// registered (mirrors WithAuth's deny-by-default).
func WithDocuments(store DocumentStore) Option {
	return func(s *Server) { s.documents = store }
}

// WithTags enables the tag autocomplete route (GET /api/v1/tags). Omitted when no
// database is configured (mirrors WithDocuments' deny-by-default: no store ⇒ no
// route). The store's tag-apply/usage_count methods are consumed by the publish
// gate directly, not through this option.
func WithTags(store TagStore) Option {
	return func(s *Server) { s.tags = store }
}

// WithPublish enables the publish/unpublish routes (POST /api/documents/:id/publish
// and .../unpublish). The service needs Postgres + the renderer, so it is omitted
// when no database is configured (deny-by-default: no service ⇒ no route).
func WithPublish(svc PublishService) Option {
	return func(s *Server) { s.publish = svc }
}

// New constructs a Server with base middleware and health routes registered.
func New(cfg config.Config, log *slog.Logger, version string, opts ...Option) *Server {
	app := fiber.New(fiber.Config{
		AppName:               "lookupkb-api",
		DisableStartupMessage: true,
		// Behind Caddy: trust proxy headers only from the reverse proxy.
		ProxyHeader: fiber.HeaderXForwardedFor,
		// fasthttp's default read buffer is 4 KiB, which the whole request head
		// (request line + headers) must fit within — otherwise it returns 431.
		// Browsers on the app origin attach the session cookie, which is chunked
		// across several cookies for real Authentik tokens (see the web BFF), so a
		// same-origin request (e.g. the health widget's /healthz) can carry a
		// multi-KiB Cookie header. Give the head generous room so those never 431.
		ReadBufferSize: 32 * 1024,
	})

	s := &Server{
		app:                 app,
		log:                 log,
		cfg:                 cfg,
		version:             version,
		readinessChecks:     map[string]ReadinessCheck{},
		informationalChecks: map[string]ReadinessCheck{},
	}
	for _, opt := range opts {
		opt(s)
	}

	// otelfiber first so its span encloses the whole request (including any
	// panic recovered below) and is the parent of downstream DB/handler spans.
	// It names spans by route template (/api/v1/me), keeping metric cardinality
	// bounded, emits RED HTTP metrics, and never logs request/response bodies.
	app.Use(otelfiber.Middleware())
	app.Use(recover.New())
	app.Use(requestid.New())
	// Lift the request id onto the span and the log context so every span
	// attribute and log line under this request carries request_id (set once).
	app.Use(injectRequestID)

	s.registerRoutes()
	return s
}

// injectRequestID copies the id generated by the requestid middleware onto the
// active span and into the logging context so it appears on every span/log line
// for the request without per-handler plumbing.
func injectRequestID(c *fiber.Ctx) error {
	rid, _ := c.Locals(requestid.ConfigDefault.ContextKey).(string)
	if rid != "" {
		ctx := logging.WithRequestID(c.UserContext(), rid)
		c.SetUserContext(ctx)
		trace.SpanFromContext(ctx).SetAttributes(attribute.String("request_id", rid))
	}
	return c.Next()
}

// Metrics exposes the scaffolded business instruments for later feature tickets.
func (s *Server) Metrics() *metrics.Metrics { return s.metrics }

// App exposes the underlying Fiber app (used by tests and main).
func (s *Server) App() *fiber.App {
	return s.app
}

// RegisterReadinessCheck adds a named dependency check that gates /readyz: if it
// fails, /readyz reports 503 (AC-11.1). Used for Postgres/Meili/Kroki/Garage.
func (s *Server) RegisterReadinessCheck(name string, check ReadinessCheck) {
	s.readinessChecks[name] = check
}

// RegisterInformationalCheck adds a named check reported by /readyz but which
// never changes the ready/not-ready verdict — for best-effort dependencies like
// the OTel collector that must not gate readiness (AC-11.1).
func (s *Server) RegisterInformationalCheck(name string, check ReadinessCheck) {
	s.informationalChecks[name] = check
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

	// Protected API. Every route under /api requires a verified token. The auth
	// middleware is attached once at the /api group so it runs a single time per
	// request (it upserts the caller's user projection, so double-attaching would
	// double that work). Only registered when auth is configured (deny-by-default:
	// no middleware ⇒ no protected surface).
	//
	// Namespace split (01-architecture.md §3): the internal UI API is unversioned
	// (/api/documents, /api/drafts/{id}); /api/v1 is reserved for the versioned
	// public read-only API (F12), which must never expose drafts — so the draft
	// routes deliberately live off /api/v1.
	if s.auth != nil {
		api := s.app.Group("/api", s.auth.Handle)

		// Versioned public surface (me today; the read-only public API grows here).
		v1 := api.Group("/v1")
		v1.Get("/me", s.handleMe)

		// Tag autocomplete (LOO-15). Only wired when a tag store is present (needs
		// Postgres); the suggestion vocabulary is owner-agnostic.
		if s.tags != nil {
			v1.Get("/tags", s.handleListTags)
		}

		// Internal draft path (LOO-11/LOO-13). Only wired when a documents store is
		// present (needs Postgres); create/save/read/list are all owner-scoped. The
		// static /drafts list is registered before /drafts/:id so it is not shadowed.
		if s.documents != nil {
			api.Post("/documents", s.handleCreateDraft)
			api.Get("/drafts", s.handleListDrafts)
			api.Get("/drafts/:id", s.handleGetDraft)
			api.Put("/drafts/:id", s.handleSaveDraft)
		}

		// Publish gate (LOO-14). The deliberate, metadata-validated draft→published
		// transition and its reverse. Only wired when the publish service is present
		// (needs Postgres + the renderer). Lives on the unversioned internal /api,
		// like the draft routes — never /api/v1.
		if s.publish != nil {
			api.Post("/documents/:id/publish", s.handlePublish)
			api.Post("/documents/:id/unpublish", s.handleUnpublish)
		}
	}
}

// handleMe returns the verified caller's identity. It proves end-to-end token
// verification (AC-1.2) and that the groups claim reaches handlers (AC-1.4); it
// is the canonical protected endpoint later tickets build on.
func (s *Server) handleMe(c *fiber.Ctx) error {
	id, ok := auth.IdentityFromCtx(c)
	if !ok {
		// The middleware guarantees this, but never assume auth state.
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}
	return c.JSON(fiber.Map{
		"id":     id.UserID,
		"sub":    id.Sub,
		"email":  id.Email,
		"groups": id.Groups,
	})
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

// handleReadyz is the readiness probe. Every gating dependency check must pass
// for a 200; any failure returns 503 (AC-11.1). Informational checks (e.g. the
// OTel collector) are reported but never change the verdict. With no checks
// registered (M0 skeleton) it reports ready.
//
// The response body reports each dependency as "ok"/"unavailable" only — the raw
// probe error (which can contain internal hostnames/URLs) is logged server-side
// but never returned, so /readyz cannot leak connection details (06-security.md).
func (s *Server) handleReadyz(c *fiber.Ctx) error {
	ctx := c.UserContext()
	checks := make(map[string]string, len(s.readinessChecks)+len(s.informationalChecks))
	ready := true

	for name, check := range s.readinessChecks {
		if err := check(); err != nil {
			ready = false
			checks[name] = "unavailable"
			s.log.WarnContext(ctx, "readiness check failed", "check", name, "error", err)
			continue
		}
		checks[name] = "ok"
	}

	// Best-effort checks are reported for visibility but do not gate readiness.
	for name, check := range s.informationalChecks {
		if err := check(); err != nil {
			checks[name] = "unavailable"
			s.log.WarnContext(ctx, "informational check failed", "check", name, "error", err)
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
