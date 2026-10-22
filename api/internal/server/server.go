// Package server wires the Fiber HTTP application. Handlers are thin: they map
// HTTP in/out and delegate to the stores and services behind the With* options.
package server

import (
	"errors"
	"log/slog"
	"time"

	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/Learistocats/LookupKB/api/internal/catalog"
	"github.com/Learistocats/LookupKB/api/internal/config"
	"github.com/Learistocats/LookupKB/api/internal/logging"
	"github.com/Learistocats/LookupKB/api/internal/mcpserver"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/google/uuid"
)

// ReadinessCheck reports whether a dependency is ready to serve traffic.
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
	// catalog is built over documents, so it exists exactly when documents does.
	catalog     *catalog.Service
	tags        TagStore
	publish     PublishService
	draftDelete DraftDeleter
	search      SearchService
	apiTokens   TokenStore
	admin       AdminService
}

// Option configures a Server at construction. Each subsystem is wired only when
// its option is given; a route is registered only when its dependency exists
// (deny-by-default).
type Option func(*Server)

// WithAuth enables the authenticated route group (/api) guarded by the token
// verification middleware. Without it no protected routes exist.
func WithAuth(mw *auth.Middleware) Option {
	return func(s *Server) { s.auth = mw }
}

// WithAccounts enables the public signup/login routes (user store + token issuer).
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

// WithDocuments enables the draft and published-read routes.
func WithDocuments(store DocumentStore) Option {
	return func(s *Server) { s.documents = store }
}

// WithDraftDelete enables DELETE /api/drafts/:id; it also needs WithDocuments.
func WithDraftDelete(svc DraftDeleter) Option {
	return func(s *Server) { s.draftDelete = svc }
}

// WithTags enables the tag autocomplete route (GET /api/v1/tags).
func WithTags(store TagStore) Option {
	return func(s *Server) { s.tags = store }
}

// WithPublish enables the publish/unpublish routes.
func WithPublish(svc PublishService) Option {
	return func(s *Server) { s.publish = svc }
}

// WithSearch enables GET /api/search and /api/v1/search.
func WithSearch(svc SearchService) Option {
	return func(s *Server) { s.search = svc }
}

// WithAdmin enables /api/admin/*. Every route is gated by auth.RequireSuperAdmin,
// re-checked from the database on each request.
func WithAdmin(svc AdminService) Option {
	return func(s *Server) { s.admin = svc }
}

// New constructs a Server with base middleware and health routes registered.
func New(cfg config.Config, log *slog.Logger, version string, opts ...Option) *Server {
	app := fiber.New(fiber.Config{
		AppName:               "lookupkb-api",
		DisableStartupMessage: true,
		// Take the client IP from X-Forwarded-For so per-IP limits hit the real
		// client, but only from loopback/private peers: a client reaching the API
		// directly cannot spoof its IP to dodge the limiter.
		ProxyHeader:             fiber.HeaderXForwardedFor,
		EnableTrustedProxyCheck: true,
		TrustedProxies:          []string{"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "::1/128", "fc00::/7"},
		// The request head must fit the read buffer (default 4 KiB, else 431); the
		// BFF's chunked session cookie needs more.
		ReadBufferSize: 32 * 1024,
		ErrorHandler:   jsonErrorHandler(log),
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
	if s.documents != nil {
		s.catalog = catalog.New(s.documents)
	}

	app.Use(recover.New())
	app.Use(requestid.New())
	app.Use(injectRequestID)

	s.registerRoutes()
	return s
}

// injectRequestID puts the request id on the logging context.
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

// RegisterReadinessCheck adds a named dependency check that gates /readyz.
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

	// Public, so registered outside the protected group; a per-IP limiter bounds
	// password guessing and signup spam.
	if s.users != nil && s.tokens != nil {
		lim := limiter.New(limiter.Config{
			Max:        10,
			Expiration: time.Minute,
			Storage:    s.limiterStorage,
			LimitReached: func(c *fiber.Ctx) error {
				return errJSON(c, fiber.StatusTooManyRequests, "too many attempts, try again later")
			},
		})
		s.app.Post("/api/v1/auth/signup", lim, s.handleSignup)
		s.app.Post("/api/v1/auth/login", lim, s.handleLogin)
	}

	if s.auth == nil {
		return
	}

	// The auth middleware runs once, at the /api group (it loads the caller's
	// groups). The PAT limiter is shared with /mcp so a token has one budget.
	patLimiter := s.patRateLimiter()
	api := s.app.Group("/api", s.auth.Handle, patLimiter)

	// /api/v1 is the read-only public surface (the PAT read surface); drafts
	// deliberately live off it, on the unversioned internal /api.
	v1 := api.Group("/v1")
	v1.Get("/me", s.handleMe)
	if s.documents != nil {
		v1.Get("/documents", s.handleV1ListDocuments)
		v1.Get("/documents/:slug", s.handleV1GetDocument)
	}
	if s.search != nil {
		v1.Get("/search", s.handleSearch)
		api.Get("/search", s.handleSearch)
	}
	if s.tags != nil {
		v1.Get("/tags", s.handleListTags)
	}

	if s.documents != nil {
		api.Post("/documents", s.handleCreateDraft)
		api.Get("/drafts", s.handleListDrafts)
		// Static segment: must precede /documents/:slug.
		api.Get("/documents/mine", s.handleListMyDocuments)
		api.Get("/drafts/:id", s.handleGetDraft)
		api.Put("/drafts/:id", s.handleSaveDraft)
		if s.draftDelete != nil {
			api.Delete("/drafts/:id", s.handleDeleteDraft)
		}
		api.Get("/documents/:slug", s.handleReadDocument)
	}

	if s.publish != nil {
		api.Post("/documents/:id/publish", s.handlePublish)
		api.Post("/documents/:id/unpublish", s.handleUnpublish)
	}

	if s.catalog != nil && s.search != nil && s.tags != nil {
		s.registerMCP(patLimiter)
	}

	// Session-only: a PAT gets 403 here (auth.PATAllowed).
	if s.apiTokens != nil {
		api.Get("/tokens", s.handleListTokens)
		api.Post("/tokens", s.handleCreateToken)
		api.Delete("/tokens/:id", s.handleRevokeToken)
	}

	if s.admin != nil {
		s.registerAdminRoutes(api.Group("/admin", auth.RequireSuperAdmin))
	}
}

// handleMe returns the verified caller's identity and group ids.
func (s *Server) handleMe(c *fiber.Ctx) error {
	id, ok := auth.IdentityFromCtx(c)
	if !ok {
		// The middleware guarantees this, but never assume auth state.
		return errJSON(c, fiber.StatusUnauthorized, "unauthorized")
	}
	groups := []string{}
	if s.users != nil {
		gids, err := s.users.ListUserGroupIDs(c.UserContext(), id.Caller().UserID)
		if err != nil {
			s.log.ErrorContext(c.UserContext(), "load caller groups failed", "error", err.Error())
			return internalError(c)
		}
		for _, g := range gids {
			groups = append(groups, uuid.UUID(g.Bytes).String())
		}
	}
	return c.JSON(fiber.Map{
		"id":     id.UserID.String(),
		"email":  id.Email,
		"groups": groups,
		// Drives the web's "Admin" link only; the API re-checks it per request.
		"is_super_admin": id.IsSuperAdmin,
	})
}

// handleHealthz is the liveness probe; it never touches dependencies.
func (s *Server) handleHealthz(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"status":  "ok",
		"service": "lookupkb-api",
		"version": s.version,
	})
}

// handleReadyz is the readiness probe: every check must pass for a 200, else 503.
// The body says only "ok"/"unavailable"; the raw probe error (which can contain
// internal hostnames) is logged, never returned.
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

// registerMCP mounts /mcp (outside /api, so it takes the auth middleware and PAT
// limiter itself). The SDK answers GET with 405 in stateless mode.
func (s *Server) registerMCP(limiter fiber.Handler) {
	h, err := mcpserver.New(s.catalog, s.search, s.tags, mcpserver.Options{
		Version:        s.version,
		AllowedOrigins: s.cfg.MCPAllowedOrigins,
		Logger:         s.log,
	})
	if err != nil {
		// Only a tool-schema bug can fail here; boot without the endpoint.
		s.log.Error("mcp endpoint disabled", "error", err.Error())
		return
	}
	s.app.All("/mcp", s.auth.Handle, limiter, h.Handle)
}

// jsonErrorHandler renders a returned *fiber.Error as {"error": msg}; any other
// error is logged and answered with a generic 500 so internals never leak.
func jsonErrorHandler(log *slog.Logger) fiber.ErrorHandler {
	return func(c *fiber.Ctx, err error) error {
		var fe *fiber.Error
		if errors.As(err, &fe) {
			return errJSON(c, fe.Code, fe.Message)
		}
		log.ErrorContext(c.UserContext(), "unhandled request error", "error", err.Error())
		return internalError(c)
	}
}
