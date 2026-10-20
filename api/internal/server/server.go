// Package server wires the Fiber HTTP application.
//
// The handler layer is deliberately thin: routes delegate to small functions
// so the framework stays swappable (the tech-stack doc keeps Echo as a
// fallback). Only liveness/readiness live here today; feature routes arrive in
// later tickets.
package server

import (
	"log/slog"
	"time"

	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/Learistocats/LookupKB/api/internal/config"
	"github.com/Learistocats/LookupKB/api/internal/logging"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

// ReadinessCheck reports whether a dependency is ready to serve traffic
// (Postgres, Meilisearch, Kroki, Garage). The map returned by /readyz is keyed by
// check name.
type ReadinessCheck func() error

// Server holds the Fiber app and its dependencies.
type Server struct {
	app     *fiber.App
	log     *slog.Logger
	cfg     config.Config
	version string
	// readinessChecks gate /readyz: any failure makes the API "not ready" (503).
	readinessChecks map[string]ReadinessCheck
	auth            *auth.Middleware
	users           UserStore
	tokens          *auth.Tokens
	limiterStorage  fiber.Storage
	documents       DocumentStore
	tags            TagStore
	publish         PublishService
	search          SearchService
	admin           AdminService
}

// Option configures a Server at construction. Later tickets add their own.
type Option func(*Server)

// WithAuth enables the authenticated route group (/api) guarded by the token
// verification middleware. Omitted when auth is not configured (dev/tests), in
// which case no protected routes are registered.
func WithAuth(mw *auth.Middleware) Option {
	return func(s *Server) { s.auth = mw }
}

// WithAccounts enables the public signup/login routes. It needs the user store
// (Postgres) and the token issuer; omitted when either is missing, in which case
// the routes are not registered (deny-by-default).
func WithAccounts(users UserStore, tokens *auth.Tokens) Option {
	return func(s *Server) {
		s.users = users
		s.tokens = tokens
	}
}

// WithLimiterStorage backs the signup/login rate limiter with shared storage
// (Valkey). When omitted the limiter keeps its counters in process memory.
func WithLimiterStorage(st fiber.Storage) Option {
	return func(s *Server) { s.limiterStorage = st }
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

// WithSearch enables GET /api/search. Omitted when Meilisearch is not configured
// (deny-by-default: no service ⇒ no route).
func WithSearch(svc SearchService) Option {
	return func(s *Server) { s.search = svc }
}

// WithAdmin enables the super-admin surface (/api/admin/*, LOO-56). It needs
// Postgres, and the routes additionally require auth; omitted otherwise
// (deny-by-default: no service ⇒ no route). Every admin route is gated by
// auth.RequireSuperAdmin, re-checked from the database on each request.
func WithAdmin(svc AdminService) Option {
	return func(s *Server) { s.admin = svc }
}

// New constructs a Server with base middleware and health routes registered.
func New(cfg config.Config, log *slog.Logger, version string, opts ...Option) *Server {
	app := fiber.New(fiber.Config{
		AppName:               "lookupkb-api",
		DisableStartupMessage: true,
		// Behind a reverse proxy (the web BFF or the host's proxy): take the client
		// IP from X-Forwarded-For so per-IP rate limits apply to the real client —
		// but only when the peer is on a loopback/private network, so a client
		// reaching the API directly cannot spoof its IP to dodge the limiter.
		ProxyHeader:             fiber.HeaderXForwardedFor,
		EnableTrustedProxyCheck: true,
		TrustedProxies:          []string{"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "::1/128", "fc00::/7"},
		// fasthttp's default read buffer is 4 KiB, which the whole request head
		// (request line + headers) must fit within — otherwise it returns 431. A
		// same-origin browser request carries the (possibly chunked) session
		// cookie, so give the head generous room so those never 431.
		ReadBufferSize: 32 * 1024,
	})

	s := &Server{
		app:             app,
		log:             log,
		cfg:             cfg,
		version:         version,
		readinessChecks: map[string]ReadinessCheck{},
	}
	for _, opt := range opts {
		opt(s)
	}

	app.Use(recover.New())
	app.Use(requestid.New())
	// Lift the request id onto the log context so every log line under this
	// request carries request_id (set once).
	app.Use(injectRequestID)

	s.registerRoutes()
	return s
}

// injectRequestID copies the id generated by the requestid middleware into the
// logging context so it appears on every log line for the request without
// per-handler plumbing.
func injectRequestID(c *fiber.Ctx) error {
	if rid, _ := c.Locals(requestid.ConfigDefault.ContextKey).(string); rid != "" {
		c.SetUserContext(logging.WithRequestID(c.UserContext(), rid))
	}
	return c.Next()
}

// App exposes the underlying Fiber app (used by tests and main).
func (s *Server) App() *fiber.App {
	return s.app
}

// RegisterReadinessCheck adds a named dependency check that gates /readyz: if it
// fails, /readyz reports 503 (AC-11.1). Used for Postgres/Meili/Kroki/Garage.
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

	// Public account routes (signup/login). Registered BEFORE the protected /api
	// group so they are matched without the auth middleware, and only when both
	// the user store and the token issuer are present (deny-by-default). A
	// per-client-IP limiter bounds password guessing and signup spam.
	if s.users != nil && s.tokens != nil {
		lim := limiter.New(limiter.Config{
			Max:        10,
			Expiration: time.Minute,
			Storage:    s.limiterStorage,
			LimitReached: func(c *fiber.Ctx) error {
				return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{"error": "too many attempts, try again later"})
			},
		})
		s.app.Post("/api/v1/auth/signup", lim, s.handleSignup)
		s.app.Post("/api/v1/auth/login", lim, s.handleLogin)
	}

	// Protected API. Every route under /api requires a verified token. The auth
	// middleware is attached once at the /api group so it runs a single time per
	// request (it loads the caller's groups, so double-attaching would double that
	// work). Only registered when auth is configured (deny-by-default:
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
			// The caller's entries for the history panel (drafts pinned first). The
			// static /documents/mine is registered before /documents/:slug.
			api.Get("/documents/mine", s.handleListMyDocuments)
			api.Get("/drafts/:id", s.handleGetDraft)
			api.Put("/drafts/:id", s.handleSaveDraft)

			// Hot read path (F8, LOO-18): serve a published document's cached,
			// sanitized HTML by slug. Distinct method (GET) + shape (:slug, not an
			// id) from POST /documents and POST /documents/:id/publish → no conflict.
			// Access is public-or-author here; group ACLs extend CanRead in M3.
			api.Get("/documents/:slug", s.handleReadDocument)
		}

		// Publish gate (LOO-14). The deliberate, metadata-validated draft→published
		// transition and its reverse. Only wired when the publish service is present
		// (needs Postgres + the renderer). Lives on the unversioned internal /api,
		// like the draft routes — never /api/v1.
		if s.search != nil {
			api.Get("/search", s.handleSearch)
		}

		if s.publish != nil {
			api.Post("/documents/:id/publish", s.handlePublish)
			api.Post("/documents/:id/unpublish", s.handleUnpublish)
		}

		// Super-admin surface (LOO-56): groups, membership, tag policy, the role
		// itself. Internal (unversioned) and gated per request by RequireSuperAdmin.
		if s.admin != nil {
			s.registerAdminRoutes(api.Group("/admin", auth.RequireSuperAdmin))
		}
	}
}

// handleMe returns the verified caller's identity. It proves end-to-end token
// verification (AC-1.2) and that group membership reaches handlers (AC-1.4); it
// is the canonical protected endpoint later tickets build on.
func (s *Server) handleMe(c *fiber.Ctx) error {
	id, ok := auth.IdentityFromCtx(c)
	if !ok {
		// The middleware guarantees this, but never assume auth state.
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}
	return c.JSON(fiber.Map{
		"id":     id.UserID,
		"email":  id.Email,
		"groups": id.Groups,
		// Drives the web's "Admin" link only; the API re-checks it per request.
		"is_super_admin": id.IsSuperAdmin,
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
// for a 200; any failure returns 503 (AC-11.1). With no checks registered it
// reports ready.
//
// The response body reports each dependency as "ok"/"unavailable" only — the raw
// probe error (which can contain internal hostnames/URLs) is logged server-side
// but never returned, so /readyz cannot leak connection details (06-security.md).
func (s *Server) handleReadyz(c *fiber.Ctx) error {
	ctx := c.UserContext()
	checks := make(map[string]string, len(s.readinessChecks))
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
