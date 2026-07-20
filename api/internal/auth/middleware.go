package auth

import (
	"context"
	"log/slog"
	"strings"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/Learistocats/LookupKB/api/internal/logging"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// identityLocalsKey keys the verified Identity on the Fiber context. Unexported
// so handlers must go through IdentityFromCtx.
const identityLocalsKey = "auth.identity"

// Identity is the verified caller projected onto the request. Groups come
// straight from the token claim (never a cached DB decision) so per-request
// authorization stays correct across the token's lifetime.
type Identity struct {
	// UserID is the LookupKB-internal users.id (uuid). Empty when the DB
	// upsert is not wired (e.g. verify-only tests).
	UserID string
	Sub    string
	Email  string
	Groups []string
}

// UserUpserter refreshes the users projection for a verified caller. *dbgen.Queries
// satisfies it; kept as an interface so the middleware is testable without a DB.
type UserUpserter interface {
	UpsertUser(ctx context.Context, arg dbgen.UpsertUserParams) (dbgen.User, error)
}

// Middleware verifies the bearer token on protected routes and injects the
// caller's Identity into the request context.
type Middleware struct {
	verifier *Verifier
	users    UserUpserter
	log      *slog.Logger
}

// NewMiddleware wires the verifier, the user-projection upsert and the logger.
// users may be nil (the upsert is skipped and UserID is left empty).
func NewMiddleware(verifier *Verifier, users UserUpserter, log *slog.Logger) *Middleware {
	return &Middleware{verifier: verifier, users: users, log: log}
}

// Handle is the Fiber handler enforcing authentication. A missing/malformed
// header, or any verification failure (bad signature, wrong issuer/audience,
// expiry), yields 401 — never a raw error and never the token in logs.
func (m *Middleware) Handle(c *fiber.Ctx) error {
	raw, ok := bearerToken(c)
	if !ok {
		return unauthorized(c)
	}

	claims, err := m.verifier.Verify(c.UserContext(), raw)
	if err != nil {
		// Deliberately does not include the token/verifier — only the reason.
		m.log.WarnContext(c.UserContext(), "rejected unauthenticated request", "error", err.Error())
		return unauthorized(c)
	}

	id := Identity{
		Sub:    claims.Subject,
		Email:  claims.Email,
		Groups: claims.Groups,
	}

	if m.users != nil {
		user, err := m.users.UpsertUser(c.UserContext(), dbgen.UpsertUserParams{
			OidcSub:     claims.Subject,
			Email:       nullableString(claims.Email),
			DisplayName: nullableString(claims.DisplayName()),
		})
		if err != nil {
			m.log.ErrorContext(c.UserContext(), "user projection upsert failed", "error", err.Error())
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal error"})
		}
		id.UserID = uuidString(user.ID)
	}

	// Attribute the request to the caller for the rest of the pipeline. Use the
	// internal user id, never the email — actor_id is redacted/pseudonymized PII
	// (06-security.md §7). Set once here and picked up by every downstream span
	// and log line without re-plumbing.
	if id.UserID != "" {
		ctx := logging.WithActorID(c.UserContext(), id.UserID)
		c.SetUserContext(ctx)
		trace.SpanFromContext(ctx).SetAttributes(attribute.String("actor_id", id.UserID))
	}

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

// nullableString maps "" to a nil *string so empty claims persist as SQL NULL.
func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// uuidString renders a pgtype.UUID as its canonical string form ("" if invalid).
func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return uuid.UUID(u.Bytes).String()
}
