package server

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/Learistocats/LookupKB/api/internal/db"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Account input bounds. The password cap keeps argon2 input bounded; the length
// floor is the only composition rule (NIST SP 800-63B favours length over rules).
const (
	minPasswordRunes   = 12
	maxPasswordBytes   = 1024
	maxEmailBytes      = 254
	maxDisplayNameRune = 100
)

// UserStore is the subset of the generated queries the signup/login and /me handlers
// need. *dbgen.Queries satisfies it; tests use a fake.
type UserStore interface {
	CreateUser(ctx context.Context, arg dbgen.CreateUserParams) (dbgen.User, error)
	GetUserByEmail(ctx context.Context, email string) (dbgen.User, error)
	TouchUserLastSeen(ctx context.Context, id pgtype.UUID) error
	ListUserGroupIDs(ctx context.Context, userID pgtype.UUID) ([]pgtype.UUID, error)
}

type credentialsRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

type userResponse struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	DisplayName *string `json:"display_name"`
}

type sessionResponse struct {
	Token     string       `json:"token"`
	ExpiresAt string       `json:"expires_at"`
	User      userResponse `json:"user"`
}

// handleSignup registers a new email/password account (open signup) and returns
// a token so the BFF can start a session immediately.
func (s *Server) handleSignup(c *fiber.Ctx) error {
	var body credentialsRequest
	if err := c.BodyParser(&body); err != nil {
		return badRequest(c, "invalid request body")
	}
	email, ok := normalizeEmail(body.Email)
	if !ok {
		return errJSON(c, fiber.StatusUnprocessableEntity, "invalid email address")
	}
	if utf8.RuneCountInString(body.Password) < minPasswordRunes || len(body.Password) > maxPasswordBytes {
		return errJSON(c, fiber.StatusUnprocessableEntity, "password must be at least 12 characters")
	}
	var displayName *string
	if dn := strings.TrimSpace(body.DisplayName); dn != "" {
		if utf8.RuneCountInString(dn) > maxDisplayNameRune {
			return errJSON(c, fiber.StatusUnprocessableEntity, "display name is too long")
		}
		displayName = &dn
	}

	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		s.log.ErrorContext(c.UserContext(), "hash password failed", "error", err.Error())
		return internalError(c)
	}

	user, err := s.users.CreateUser(c.UserContext(), dbgen.CreateUserParams{
		Email:        email,
		PasswordHash: hash,
		DisplayName:  displayName,
	})
	if db.IsUniqueViolation(err) {
		return errJSON(c, fiber.StatusConflict, "an account with this email already exists")
	}
	if err != nil {
		s.log.ErrorContext(c.UserContext(), "create user failed", "error", err.Error())
		return internalError(c)
	}

	return s.issueSession(c, fiber.StatusCreated, user)
}

// handleLogin exchanges email/password for a token. Unknown email, wrong
// password and an account without a usable hash all return the SAME 401 after
// the same argon2 work, so the response never reveals whether an email exists.
func (s *Server) handleLogin(c *fiber.Ctx) error {
	var body credentialsRequest
	if err := c.BodyParser(&body); err != nil {
		return badRequest(c, "invalid request body")
	}
	email, _ := normalizeEmail(body.Email)
	if len(body.Password) > maxPasswordBytes {
		return invalidCredentials(c)
	}

	user, err := s.users.GetUserByEmail(c.UserContext(), email)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && user.PasswordHash == "") {
		auth.VerifyDummy(body.Password)
		return invalidCredentials(c)
	}
	if err != nil {
		s.log.ErrorContext(c.UserContext(), "lookup user failed", "error", err.Error())
		return internalError(c)
	}
	if !auth.VerifyPassword(user.PasswordHash, body.Password) {
		return invalidCredentials(c)
	}

	if err := s.users.TouchUserLastSeen(c.UserContext(), user.ID); err != nil {
		// Non-fatal: last_seen_at is informational only.
		s.log.WarnContext(c.UserContext(), "touch last_seen_at failed", "error", err.Error())
	}
	return s.issueSession(c, fiber.StatusOK, user)
}

func (s *Server) issueSession(c *fiber.Ctx, status int, user dbgen.User) error {
	id := formatUUID(user.ID)
	token, exp, err := s.tokens.Issue(id, user.Email)
	if err != nil {
		s.log.ErrorContext(c.UserContext(), "issue token failed", "error", err.Error())
		return internalError(c)
	}
	return c.Status(status).JSON(sessionResponse{
		Token:     token,
		ExpiresAt: exp.UTC().Format(time.RFC3339),
		User:      userResponse{ID: id, Email: user.Email, DisplayName: user.DisplayName},
	})
}

func invalidCredentials(c *fiber.Ctx) error {
	return errJSON(c, fiber.StatusUnauthorized, "invalid email or password")
}

// normalizeEmail trims and validates a bare address ("Name <a@b>" forms are
// rejected). Case is preserved; the citext column compares case-insensitively.
func normalizeEmail(raw string) (string, bool) {
	email := strings.TrimSpace(raw)
	if email == "" || len(email) > maxEmailBytes {
		return "", false
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return "", false
	}
	return email, true
}
