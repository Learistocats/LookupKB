package server

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// Personal Access Token management. These routes are session-only: a PAT
// reaching them gets 403 from the auth middleware (auth.PATAllowed).
const (
	// maxActiveTokens caps a user's live (unrevoked, unexpired) tokens.
	maxActiveTokens = 20
	// maxTokenNameLen bounds the user-chosen label (in characters).
	maxTokenNameLen = 100
	// maxTokenExpiryDays bounds the optional expiry.
	maxTokenExpiryDays = 365
	// maxCreateAttempts bounds retries on a token-prefix collision.
	maxCreateAttempts = 3
	// patRateLimit is the per-token request budget per patRateWindow.
	patRateLimit  = 120
	patRateWindow = time.Minute
)

// TokenStore is the subset of the generated queries the token routes need.
// *dbgen.Queries satisfies it. Create (which enforces the active-token cap) and
// revoke write their audit row in the same statement (see queries/api_tokens.sql).
type TokenStore interface {
	CreateAPIToken(ctx context.Context, arg dbgen.CreateAPITokenParams) (dbgen.CreateAPITokenRow, error)
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

func newTokenResponse(id pgtype.UUID, name, prefix string, created, lastUsed, expires, revoked pgtype.Timestamptz) tokenResponse {
	return tokenResponse{
		ID:         formatUUID(id),
		Name:       name,
		Prefix:     prefix,
		CreatedAt:  formatTime(created),
		LastUsedAt: optTime(lastUsed),
		ExpiresAt:  optTime(expires),
		RevokedAt:  optTime(revoked),
	}
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
		out = append(out, newTokenResponse(r.ID, r.Name, r.Prefix, r.CreatedAt, r.LastUsedAt, r.ExpiresAt, r.RevokedAt))
	}
	return c.JSON(out)
}

type createTokenRequest struct {
	Name          string `json:"name"`
	ExpiresInDays *int   `json:"expires_in_days"`
}

// validate returns the trimmed name and the optional expiry, or a client-facing
// message.
func (r createTokenRequest) validate() (name string, expires pgtype.Timestamptz, msg string) {
	name = strings.TrimSpace(r.Name)
	switch {
	case name == "":
		return "", expires, "name is required"
	case utf8.RuneCountInString(name) > maxTokenNameLen:
		return "", expires, "name is too long"
	}
	if r.ExpiresInDays != nil {
		d := *r.ExpiresInDays
		if d < 1 || d > maxTokenExpiryDays {
			return "", expires, "expires_in_days must be between 1 and 365"
		}
		expires = pgtype.Timestamptz{Time: time.Now().Add(time.Duration(d) * 24 * time.Hour), Valid: true}
	}
	return name, expires, ""
}

// handleCreateToken issues a PAT and returns its plaintext exactly once (201).
func (s *Server) handleCreateToken(c *fiber.Ctx) error {
	uid, err := s.callerID(c)
	if err != nil {
		return err
	}
	var body createTokenRequest
	if err := c.BodyParser(&body); err != nil {
		return badRequest(c, "invalid request body")
	}
	name, expires, msg := body.validate()
	if msg != "" {
		return badRequest(c, msg)
	}

	ctx := c.UserContext()
	// The prefix is UNIQUE; on the (astronomically rare) collision, try again with
	// a fresh token, up to maxCreateAttempts in all.
	for attempt := 1; ; attempt++ {
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
			MaxActive: maxActiveTokens,
		})
		var pgErr *pgconn.PgError
		switch {
		case errors.Is(err, pgx.ErrNoRows): // the INSERT refused: at the cap
			return errJSON(c, fiber.StatusConflict, "too many active tokens; revoke one first")
		case errors.As(err, &pgErr) && pgErr.Code == "23505" && attempt < maxCreateAttempts:
			continue
		case err != nil:
			s.log.ErrorContext(ctx, "create api token failed", "error", err.Error())
			return internalError(c)
		}
		s.log.InfoContext(ctx, "api token created", "token_id", formatUUID(row.ID), "prefix", row.Prefix)
		resp := newTokenResponse(row.ID, row.Name, row.Prefix, row.CreatedAt, row.LastUsedAt, row.ExpiresAt, row.RevokedAt)
		resp.Token = pat.Token
		return c.Status(fiber.StatusCreated).JSON(resp)
	}
}

// handleRevokeToken revokes one of the caller's tokens. Missing, foreign and
// already-revoked tokens all answer the same 404 (no existence leak).
func (s *Server) handleRevokeToken(c *fiber.Ctx) error {
	id, ok := parseUUIDParam(c, "id")
	if !ok {
		return badRequest(c, "invalid token id")
	}
	uid, err := s.callerID(c)
	if err != nil {
		return err
	}
	_, err = s.apiTokens.RevokeMyAPIToken(c.UserContext(), dbgen.RevokeMyAPITokenParams{ID: id, UserID: uid})
	if errors.Is(err, pgx.ErrNoRows) {
		return notFound(c, "token")
	}
	if err != nil {
		s.log.ErrorContext(c.UserContext(), "revoke api token failed", "error", err.Error())
		return internalError(c)
	}
	s.log.InfoContext(c.UserContext(), "api token revoked", "token_id", formatUUID(id))
	return c.SendStatus(fiber.StatusNoContent)
}

// patRateLimiter bounds each PAT to patRateLimit requests per patRateWindow,
// keyed by token id in the shared limiter storage (Valkey, or process
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
			return errJSON(c, fiber.StatusTooManyRequests, "rate limit exceeded, try again later")
		},
	})
}
