package auth

import (
	"context"
	"log/slog"
	"strings"

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
	// IsSuperAdmin is users.is_super_admin, re-read from the database on every
	// request (never a token claim) so a revoke is honored immediately. False when
	// no database is wired (deny-by-default). See superadmin.go.
	IsSuperAdmin bool
}

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
}

// NewMiddleware wires the token verifier, the group lookup and the logger.
// groups may be nil (no database): the caller then has no groups.
func NewMiddleware(tokens *Tokens, groups GroupLister, log *slog.Logger) *Middleware {
	return &Middleware{tokens: tokens, groups: groups, log: log}
}

// Handle is the Fiber handler enforcing authentication. A missing/malformed
// header, or any verification failure (bad signature, wrong issuer, expiry),
// yields 401 — never a raw error and never the token in logs.
func (m *Middleware) Handle(c *fiber.Ctx) error {
	raw, ok := bearerToken(c)
	if !ok {
		return unauthorized(c)
	}

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

	id := Identity{UserID: uid.String(), Email: claims.Email, Groups: []string{}}

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

	if id.IsSuperAdmin, err = m.loadSuperAdmin(c.UserContext(), uid); err != nil {
		m.log.ErrorContext(c.UserContext(), "load caller super-admin flag failed", "error", err.Error())
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal error"})
	}

	// Attribute the request to the caller for the rest of the pipeline. Use the
	// internal user id, never the email — actor_id is redacted PII (06-security.md
	// §7). Set once here and picked up by every downstream log line.
	c.SetUserContext(logging.WithActorID(c.UserContext(), id.UserID))

	c.Locals(identityLocalsKey, id)
	return c.Next()
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
