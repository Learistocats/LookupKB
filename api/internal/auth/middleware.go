package auth

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/Learistocats/LookupKB/api/internal/logging"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// identityLocalsKey keys the verified Identity on the Fiber context. Unexported
// so handlers must go through IdentityFromCtx.
const identityLocalsKey = "auth.identity"

// Identity is the verified caller projected onto the request. Groups are loaded
// from group_members on every request (never baked into the token) so a
// membership change is honored immediately by access checks.
type Identity struct {
	// UserID is the LookupKB-internal users.id (uuid).
	UserID string
	Email  string
	// Groups are the caller's group ids.
	Groups []string
	// Method is how the caller authenticated: MethodSession (the BFF's JWT) or
	// MethodPAT (a Personal Access Token). Both resolve to the same user and the
	// same per-request group loading; a PAT is additionally confined to the read
	// surface (PATAllowed).
	Method string
	// TokenID is the api_tokens.id of the PAT used (empty for sessions). It keys
	// the per-token rate limit.
	TokenID string
	// IsSuperAdmin is users.is_super_admin, re-read from the database on every
	// request (never a token claim) so a revoke is honored immediately. False when
	// no database is wired (deny-by-default). See superadmin.go.
	IsSuperAdmin bool
}

// Authentication methods recorded on Identity.Method.
const (
	MethodSession = "session"
	MethodPAT     = "pat"
)

// GroupLister loads a user's group ids. *dbgen.Queries satisfies it; kept as an
// interface so the middleware is testable without a DB.
type GroupLister interface {
	ListUserGroupIDs(ctx context.Context, userID pgtype.UUID) ([]pgtype.UUID, error)
}

// Middleware verifies the bearer token on protected routes and injects the
// caller's Identity into the request context.
type Middleware struct {
	tokens *Tokens
	groups GroupLister
	log    *slog.Logger
	// pats verifies Personal Access Tokens. nil (no database) ⇒ every PAT is
	// rejected with 401 (deny-by-default).
	pats PATVerifier
	now  func() time.Time
}

// NewMiddleware wires the token verifier, the group lookup and the logger.
// groups may be nil (no database): the caller then has no groups.
func NewMiddleware(tokens *Tokens, groups GroupLister, log *slog.Logger) *Middleware {
	return &Middleware{tokens: tokens, groups: groups, log: log, now: time.Now}
}

// WithPATs enables Personal Access Token authentication. Wired only when the
// token store (Postgres) exists; without it every PAT is rejected with 401.
func (m *Middleware) WithPATs(v PATVerifier) *Middleware {
	m.pats = v
	return m
}

// Handle is the Fiber handler enforcing authentication. A missing/malformed
// header, or any verification failure (bad signature, wrong issuer, expiry),
// yields 401 — never a raw error and never the token in logs.
func (m *Middleware) Handle(c *fiber.Ctx) error {
	raw, ok := bearerToken(c)
	if !ok {
		return unauthorized(c)
	}

	var (
		uid uuid.UUID
		id  Identity
	)
	if IsPAT(raw) {
		// Personal Access Token: verified against the store and confined to the
		// read surface, then falls into the same group loading as a session.
		rec, ok, err := m.authenticatePAT(c, raw)
		if !ok {
			return err
		}
		uid = rec.UserID
		id = Identity{UserID: uid.String(), Email: rec.Email, Groups: []string{}, Method: MethodPAT, TokenID: rec.ID.String()}
	} else {
		claims, err := m.tokens.Verify(raw)
		if err != nil {
			// Deliberately does not include the token — only the reason.
			m.log.WarnContext(c.UserContext(), "rejected unauthenticated request", "error", err.Error())
			return unauthorized(c)
		}
		uid, err = uuid.Parse(claims.UserID)
		if err != nil {
			m.log.WarnContext(c.UserContext(), "rejected token with non-uuid subject")
			return unauthorized(c)
		}
		id = Identity{UserID: uid.String(), Email: claims.Email, Groups: []string{}, Method: MethodSession}
	}

	if m.groups != nil {
		gids, err := m.groups.ListUserGroupIDs(c.UserContext(), pgtype.UUID{Bytes: uid, Valid: true})
		if err != nil {
			m.log.ErrorContext(c.UserContext(), "load caller groups failed", "error", err.Error())
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal error"})
		}
		for _, g := range gids {
			id.Groups = append(id.Groups, uuid.UUID(g.Bytes).String())
		}
	}

	isAdmin, err := m.loadSuperAdmin(c.UserContext(), uid)
	if err != nil {
		m.log.ErrorContext(c.UserContext(), "load caller super-admin flag failed", "error", err.Error())
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal error"})
	}
	id.IsSuperAdmin = isAdmin

	// Attribute the request to the caller for the rest of the pipeline. Use the
	// internal user id, never the email — actor_id is redacted PII (06-security.md
	// §7). Set once here and picked up by every downstream log line.
	c.SetUserContext(logging.WithActorID(c.UserContext(), id.UserID))

	c.Locals(identityLocalsKey, id)
	return c.Next()
}

// authenticatePAT verifies a PAT and enforces the read-surface restriction. ok
// is false when the request was rejected; the response is then already written
// (401 with the same body as a bad JWT, 403 off the read surface, 500 on a store
// error) and err is what Handle must return.
func (m *Middleware) authenticatePAT(c *fiber.Ctx, raw string) (PATRecord, bool, error) {
	ctx := c.UserContext()
	if m.pats == nil {
		m.log.WarnContext(ctx, "rejected personal access token: PATs not configured")
		return PATRecord{}, false, unauthorized(c)
	}
	rec, err := verifyPAT(ctx, m.pats, raw, m.now())
	if errors.Is(err, ErrInvalidPAT) {
		// The reason only — never the token.
		m.log.WarnContext(ctx, "rejected personal access token", "error", err.Error())
		return PATRecord{}, false, unauthorized(c)
	}
	if err != nil {
		m.log.ErrorContext(ctx, "personal access token lookup failed", "error", err.Error())
		return PATRecord{}, false, c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal error"})
	}
	if !PATAllowed(c.Method(), c.Path()) {
		m.log.WarnContext(ctx, "personal access token used off the read surface",
			"token_id", rec.ID.String(), "method", c.Method(), "path", c.Path())
		return PATRecord{}, false, c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "forbidden"})
	}
	// Throttled usage stamp (the query guards the write too). A failure is logged,
	// never fatal to the request.
	if rec.LastUsedAt.IsZero() || m.now().Sub(rec.LastUsedAt) >= lastUsedGranularity {
		if err := m.pats.TouchPAT(ctx, rec.ID); err != nil {
			m.log.WarnContext(ctx, "touch personal access token failed", "error", err.Error())
		}
	}
	return rec, true, nil
}

// IdentityFromCtx returns the verified Identity set by the middleware. ok is
// false on unauthenticated requests (routes that skipped the middleware).
func IdentityFromCtx(c *fiber.Ctx) (Identity, bool) {
	id, ok := c.Locals(identityLocalsKey).(Identity)
	return id, ok
}

// bearerToken extracts the token from an "Authorization: Bearer <token>" header
// (scheme match is case-insensitive per RFC 6750).
func bearerToken(c *fiber.Ctx) (string, bool) {
	header := c.Get(fiber.HeaderAuthorization)
	if header == "" {
		return "", false
	}
	const prefix = "bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(header[len(prefix):])
	if token == "" {
		return "", false
	}
	return token, true
}

func unauthorized(c *fiber.Ctx) error {
	return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
}
