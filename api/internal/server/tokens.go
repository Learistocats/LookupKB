package server

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/Learistocats/LookupKB/api/internal/db"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Personal Access Token management (LOO-37/LOO-42). These routes live on the
// unversioned internal /api and are session-only: a PAT reaching them gets 403
// from the auth middleware's read-surface check (auth.PATAllowed).
const (
	// maxActiveTokens caps a user's live (unrevoked, unexpired) tokens.
	maxActiveTokens = 20
	// maxTokenNameLen bounds the user-chosen label (in characters).
	maxTokenNameLen = 100
	// maxTokenExpiryDays bounds the optional expiry.
	maxTokenExpiryDays = 365
	// patRateLimit is the per-token request budget per patRateWindow (AC-12.10).
	patRateLimit  = 120
	patRateWindow = time.Minute
)

// TokenStore is the subset of the generated queries the token routes need.
// *dbgen.Queries satisfies it. Create and revoke write their audit row in the
// same statement (see queries/api_tokens.sql).
type TokenStore interface {
	CreateAPIToken(ctx context.Context, arg dbgen.CreateAPITokenParams) (dbgen.CreateAPITokenRow, error)
	CountActiveAPITokens(ctx context.Context, userID pgtype.UUID) (int64, error)
	ListMyAPITokens(ctx context.Context, userID pgtype.UUID) ([]dbgen.ListMyAPITokensRow, error)
	RevokeMyAPIToken(ctx context.Context, arg dbgen.RevokeMyAPITokenParams) (pgtype.UUID, error)
}

// WithTokens enables the PAT management routes (GET/POST /api/tokens, DELETE
// /api/tokens/:id). Omitted without a database (deny-by-default: no store ⇒ no
// route).
func WithTokens(store TokenStore) Option {
	return func(s *Server) { s.apiTokens = store }
}

// tokenResponse is a token as the owner sees it: never the hash, and the
// plaintext only on the creation response (Token).
type tokenResponse struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
	ExpiresAt  *string `json:"expires_at"`
	RevokedAt  *string `json:"revoked_at"`
	Token      string  `json:"token,omitempty"`
}

func optTime(t pgtype.Timestamptz) *string {
	if !t.Valid {
		return nil
	}
	s := formatTime(t)
	return &s
}

func (s *Server) handleListTokens(c *fiber.Ctx) error {
	uid, err := s.callerID(c)
	if err != nil {
		return err
	}
	rows, err := s.apiTokens.ListMyAPITokens(c.UserContext(), uid)
	if err != nil {
		s.log.ErrorContext(c.UserContext(), "list api tokens failed", "error", err.Error())
		return internalError(c)
	}
	out := make([]tokenResponse, 0, len(rows))
	for _, r := range rows {
		out = append(out, tokenResponse{
			ID:         formatUUID(r.ID),
			Name:       r.Name,
			Prefix:     r.Prefix,
			CreatedAt:  formatTime(r.CreatedAt),
			LastUsedAt: optTime(r.LastUsedAt),
			ExpiresAt:  optTime(r.ExpiresAt),
			RevokedAt:  optTime(r.RevokedAt),
		})
	}
	return c.JSON(out)
}

// handleCreateToken issues a PAT and returns its plaintext exactly once (201).
func (s *Server) handleCreateToken(c *fiber.Ctx) error {
	uid, err := s.callerID(c)
	if err != nil {
		return err
	}
	var body struct {
		Name          string `json:"name"`
		ExpiresInDays *int   `json:"expires_in_days"`
	}
	if err := c.BodyParser(&body); err != nil {
		return badRequest(c, "invalid request body")
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		return badRequest(c, "name is required")
	}
	if utf8.RuneCountInString(name) > maxTokenNameLen {
		return badRequest(c, "name is too long")
	}
	var expires pgtype.Timestamptz
	if body.ExpiresInDays != nil {
		d := *body.ExpiresInDays
		if d < 1 || d > maxTokenExpiryDays {
			return badRequest(c, "expires_in_days must be between 1 and 365")
		}
		expires = pgtype.Timestamptz{Time: time.Now().Add(time.Duration(d) * 24 * time.Hour), Valid: true}
	}

	ctx := c.UserContext()
	n, err := s.apiTokens.CountActiveAPITokens(ctx, uid)
	if err != nil {
		s.log.ErrorContext(ctx, "count api tokens failed", "error", err.Error())
		return internalError(c)
	}
	if n >= maxActiveTokens {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "too many active tokens; revoke one first"})
	}

	// The prefix is UNIQUE; retry the (astronomically rare) collision once more.
	for attempt := 0; ; attempt++ {
		pat, err := auth.GeneratePAT()
		if err != nil {
			s.log.ErrorContext(ctx, "generate api token failed", "error", err.Error())
			return internalError(c)
		}
		row, err := s.apiTokens.CreateAPIToken(ctx, dbgen.CreateAPITokenParams{
			UserID:    uid,
			Name:      name,
			TokenHash: pat.Hash,
			Prefix:    pat.Prefix,
			ExpiresAt: expires,
		})
		if db.IsUniqueViolation(err) && attempt < 2 {
			continue
		}
		if err != nil {
			s.log.ErrorContext(ctx, "create api token failed", "error", err.Error())
			return internalError(c)
		}
		s.log.InfoContext(ctx, "api token created", "token_id", formatUUID(row.ID), "prefix", row.Prefix)
		return c.Status(fiber.StatusCreated).JSON(tokenResponse{
			ID:         formatUUID(row.ID),
			Name:       row.Name,
			Prefix:     row.Prefix,
			CreatedAt:  formatTime(row.CreatedAt),
			LastUsedAt: optTime(row.LastUsedAt),
			ExpiresAt:  optTime(row.ExpiresAt),
			RevokedAt:  optTime(row.RevokedAt),
			Token:      pat.Token,
		})
	}
}

// handleRevokeToken revokes one of the caller's tokens. Missing, foreign and
// already-revoked tokens all answer the same 404 (no existence leak).
func (s *Server) handleRevokeToken(c *fiber.Ctx) error {
	id, err := parseUUID(c.Params("id"))
	if err != nil {
		return tokenNotFound(c)
	}
	uid, err := s.callerID(c)
	if err != nil {
		return err
	}
	_, err = s.apiTokens.RevokeMyAPIToken(c.UserContext(), dbgen.RevokeMyAPITokenParams{ID: id, UserID: uid})
	if errors.Is(err, pgx.ErrNoRows) {
		return tokenNotFound(c)
	}
	if err != nil {
		s.log.ErrorContext(c.UserContext(), "revoke api token failed", "error", err.Error())
		return internalError(c)
	}
	s.log.InfoContext(c.UserContext(), "api token revoked", "token_id", formatUUID(id))
	return c.SendStatus(fiber.StatusNoContent)
}

func tokenNotFound(c *fiber.Ctx) error {
	return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "token not found"})
}

// patRateLimiter bounds each PAT to patRateLimit requests per patRateWindow,
// keyed by token id (AC-12.10) in the shared limiter storage (Valkey, or process
// memory). Session requests skip it. Exceeding it answers 429 with the
// limiter's X-RateLimit-* and Retry-After headers.
func (s *Server) patRateLimiter() fiber.Handler {
	return limiter.New(limiter.Config{
		Max:        patRateLimit,
		Expiration: patRateWindow,
		Storage:    s.limiterStorage,
		Next: func(c *fiber.Ctx) bool {
			id, ok := auth.IdentityFromCtx(c)
			return !ok || id.Method != auth.MethodPAT
		},
		KeyGenerator: func(c *fiber.Ctx) string {
			id, _ := auth.IdentityFromCtx(c)
			return "pat:" + id.TokenID
		},
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{"error": "rate limit exceeded, try again later"})
		},
	})
}
