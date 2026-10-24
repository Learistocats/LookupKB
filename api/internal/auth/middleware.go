package auth

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/Learistocats/LookupKB/api/internal/access"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/Learistocats/LookupKB/api/internal/logging"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// identityLocalsKey keys the verified Identity on the Fiber context. Unexported
// so handlers must go through IdentityFromCtx.
const identityLocalsKey = "auth.identity"

// Identity is the verified caller projected onto the request.
type Identity struct {
	UserID uuid.UUID
	Email  string
	// Method is MethodSession (the BFF's JWT) or MethodPAT. A PAT acts as its
	// user but is confined to the read surface (PATAllowed).
	Method string
	// TokenID is the api_tokens.id of the PAT used (empty for sessions). It keys
	// the per-token rate limit.
	TokenID string
	// IsSuperAdmin is users.is_super_admin, re-read from the database on every
	// request (never a token claim) so a revoke is honored immediately. False
	// when no store is wired.
	IsSuperAdmin bool
}

// Caller projects the identity onto the access-policy input.
func (id Identity) Caller() access.Caller {
	return access.Caller{UserID: pgtype.UUID{Bytes: id.UserID, Valid: true}, IsSuperAdmin: id.IsSuperAdmin}
}

// Authentication methods recorded on Identity.Method.
const (
	MethodSession = "session"
	MethodPAT     = "pat"
)

// Store is what the middleware reads per request. *dbgen.Queries satisfies it.
type Store interface {
	GetUserIsSuperAdmin(ctx context.Context, userID pgtype.UUID) (bool, error)
	GetActiveAPITokenByPrefix(ctx context.Context, prefix string) (dbgen.GetActiveAPITokenByPrefixRow, error)
	TouchAPITokenLastUsed(ctx context.Context, id pgtype.UUID) error
}

// Middleware verifies the bearer token on protected routes and injects the
// caller's Identity into the request context.
type Middleware struct {
	tokens *Tokens
	// store may be nil (no database): nobody is a super-admin and every PAT is
	// rejected with 401 (deny-by-default).
	store Store
	log   *slog.Logger
	now   func() time.Time
}

// NewMiddleware wires the token verifier, the store and the logger.
func NewMiddleware(tokens *Tokens, store Store, log *slog.Logger) *Middleware {
	return &Middleware{tokens: tokens, store: store, log: log, now: time.Now}
}

// Handle is the Fiber handler enforcing authentication. A missing/malformed
// header, or any verification failure (bad signature, wrong issuer, expiry),
// yields 401 — never a raw error and never the token in logs.
func (m *Middleware) Handle(c *fiber.Ctx) error {
	raw, ok := bearerToken(c)
	if !ok {
		return unauthorized(c)
	}

	var id Identity
	if IsPAT(raw) {
		var ok bool
		var err error
		if id, ok, err = m.authenticatePAT(c, raw); !ok {
			return err
		}
	} else {
		claims, err := m.tokens.Verify(raw)
		if err != nil {
			// Deliberately does not include the token — only the reason.
			m.log.WarnContext(c.UserContext(), "rejected unauthenticated request", "error", err.Error())
			return unauthorized(c)
		}
		uid, err := uuid.Parse(claims.UserID)
		if err != nil {
			m.log.WarnContext(c.UserContext(), "rejected token with non-uuid subject")
			return unauthorized(c)
		}
		id = Identity{UserID: uid, Email: claims.Email, Method: MethodSession}
		if id.IsSuperAdmin, err = m.loadSuperAdmin(c.UserContext(), uid); err != nil {
			m.log.ErrorContext(c.UserContext(), "load caller super-admin flag failed", "error", err.Error())
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal error"})
		}
	}

	// Attribute the request to the caller for every downstream log line. The
	// internal user id, never the email: actor_id is redacted PII.
	c.SetUserContext(logging.WithActorID(c.UserContext(), id.UserID.String()))

	c.Locals(identityLocalsKey, id)
	return c.Next()
}

// authenticatePAT verifies a PAT and enforces the read-surface restriction. ok
// is false when the request was rejected; the response is then already written
// (401 with the same body as a bad JWT, 403 off the read surface, 500 on a store
// error) and err is what Handle must return.
func (m *Middleware) authenticatePAT(c *fiber.Ctx, raw string) (Identity, bool, error) {
	ctx := c.UserContext()
	if m.store == nil {
		m.log.WarnContext(ctx, "rejected personal access token: PATs not configured")
		return Identity{}, false, unauthorized(c)
	}
	row, err := verifyPAT(ctx, m.store, raw, m.now())
	if errors.Is(err, ErrInvalidPAT) {
		// The reason only — never the token.
		m.log.WarnContext(ctx, "rejected personal access token", "error", err.Error())
		return Identity{}, false, unauthorized(c)
	}
	if err != nil {
		m.log.ErrorContext(ctx, "personal access token lookup failed", "error", err.Error())
		return Identity{}, false, c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal error"})
	}
	tokenID := uuid.UUID(row.ID.Bytes).String()
	if !PATAllowed(c.Method(), c.Path()) {
		m.log.WarnContext(ctx, "personal access token used off the read surface",
			"token_id", tokenID, "method", c.Method(), "path", c.Path())
		return Identity{}, false, c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "forbidden"})
	}
	// Throttled usage stamp; a failure is logged, never fatal to the request.
	if !row.LastUsedAt.Valid || m.now().Sub(row.LastUsedAt.Time) >= lastUsedGranularity {
		if err := m.store.TouchAPITokenLastUsed(ctx, row.ID); err != nil {
			m.log.WarnContext(ctx, "touch personal access token failed", "error", err.Error())
		}
	}
	return Identity{
		UserID:       uuid.UUID(row.UserID.Bytes),
		Email:        row.Email,
		Method:       MethodPAT,
		TokenID:      tokenID,
		IsSuperAdmin: row.IsSuperAdmin,
	}, true, nil
}

// IdentityFromCtx returns the verified Identity set by the middleware. ok is
// false on unauthenticated requests (routes that skipped the middleware).
func IdentityFromCtx(c *fiber.Ctx) (Identity, bool) {
	id, ok := c.Locals(identityLocalsKey).(Identity)
	return id, ok
}

type identityCtxKey struct{}

// ContextWithIdentity carries a verified Identity on a context.Context, for code
// that runs outside Fiber (the MCP handler is a net/http handler bridged from a
// Fiber route; c.Locals do not travel with the converted request). Only call it
// with an Identity obtained from IdentityFromCtx.
func ContextWithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityCtxKey{}, id)
}

// IdentityFromContext returns the Identity set by ContextWithIdentity. ok is false
// when there is none (fail closed: callers must treat that as unauthenticated).
func IdentityFromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(identityCtxKey{}).(Identity)
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
