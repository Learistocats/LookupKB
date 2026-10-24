package auth

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

const testSecret = "0123456789abcdef0123456789abcdef"

// fakeStore is an in-memory auth.Store.
type fakeStore struct {
	super     bool
	superErr  error
	superHits int

	rows      map[string]dbgen.GetActiveAPITokenByPrefixRow // by prefix
	lookupErr error
	touched   []pgtype.UUID
}

func (f *fakeStore) GetUserIsSuperAdmin(context.Context, pgtype.UUID) (bool, error) {
	f.superHits++
	return f.super, f.superErr
}

func (f *fakeStore) GetActiveAPITokenByPrefix(_ context.Context, prefix string) (dbgen.GetActiveAPITokenByPrefixRow, error) {
	if f.lookupErr != nil {
		return dbgen.GetActiveAPITokenByPrefixRow{}, f.lookupErr
	}
	r, ok := f.rows[prefix]
	if !ok {
		return dbgen.GetActiveAPITokenByPrefixRow{}, pgx.ErrNoRows
	}
	return r, nil
}

func (f *fakeStore) TouchAPITokenLastUsed(_ context.Context, id pgtype.UUID) error {
	f.touched = append(f.touched, id)
	return nil
}

func newTestTokens(t *testing.T) *Tokens {
	t.Helper()
	tk, err := NewTokens(testSecret, time.Hour)
	require.NoError(t, err)
	return tk
}

func newTestApp(mw *Middleware) *fiber.App {
	app := fiber.New()
	app.Get("/me", mw.Handle, func(c *fiber.Ctx) error {
		id, ok := IdentityFromCtx(c)
		if !ok {
			return c.SendStatus(fiber.StatusInternalServerError)
		}
		return c.JSON(id)
	})
	return app
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func doGet(t *testing.T, app *fiber.App, authHeader string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, "/me", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	return resp
}

func TestMiddleware_MissingHeaderIs401(t *testing.T) {
	st := &fakeStore{}
	app := newTestApp(NewMiddleware(newTestTokens(t), st, discardLogger()))

	resp := doGet(t, app, "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.Zero(t, st.superHits, "no token must not touch the database")
}

func TestMiddleware_MalformedHeaderIs401(t *testing.T) {
	app := newTestApp(NewMiddleware(newTestTokens(t), &fakeStore{}, discardLogger()))

	for _, h := range []string{"Basic abc", "Bearer", "Bearer ", "token xyz", "Bearer not.a.jwt"} {
		resp := doGet(t, app, h)
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode, "header %q", h)
		_ = resp.Body.Close()
	}
}

func TestMiddleware_ValidTokenPopulatesIdentity(t *testing.T) {
	tk := newTestTokens(t)
	uid := uuid.New()
	st := &fakeStore{}
	app := newTestApp(NewMiddleware(tk, st, discardLogger()))

	raw, _, err := tk.Issue(uid.String(), "user@example.com")
	require.NoError(t, err)
	resp := doGet(t, app, "Bearer "+raw)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var got Identity
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Equal(t, uid, got.UserID)
	require.Equal(t, "user@example.com", got.Email)
	require.Equal(t, MethodSession, got.Method)
	require.Equal(t, 1, st.superHits, "a session request is exactly one DB read")
}

func TestMiddleware_ForeignSecretIs401(t *testing.T) {
	st := &fakeStore{}
	app := newTestApp(NewMiddleware(newTestTokens(t), st, discardLogger()))

	other, err := NewTokens(strings.Repeat("x", MinSecretBytes), time.Hour)
	require.NoError(t, err)
	raw, _, err := other.Issue(uuid.NewString(), "user@example.com")
	require.NoError(t, err)

	resp := doGet(t, app, "Bearer "+raw)
	defer resp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.Zero(t, st.superHits, "an unverified token must never reach the DB")
}
