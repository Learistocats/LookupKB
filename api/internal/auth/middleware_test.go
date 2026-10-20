package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

const testSecret = "0123456789abcdef0123456789abcdef"

// fakeGroups records the lookup and returns canned group ids/error.
type fakeGroups struct {
	called bool
	gotID  pgtype.UUID
	ids    []pgtype.UUID
	err    error
}

func (f *fakeGroups) ListUserGroupIDs(_ context.Context, userID pgtype.UUID) ([]pgtype.UUID, error) {
	f.called = true
	f.gotID = userID
	return f.ids, f.err
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
	g := &fakeGroups{}
	app := newTestApp(NewMiddleware(newTestTokens(t), g, discardLogger()))

	resp := doGet(t, app, "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.False(t, g.called, "no token must not touch the database")
}

func TestMiddleware_MalformedHeaderIs401(t *testing.T) {
	app := newTestApp(NewMiddleware(newTestTokens(t), &fakeGroups{}, discardLogger()))

	for _, h := range []string{"Basic abc", "Bearer", "Bearer ", "token xyz", "Bearer not.a.jwt"} {
		resp := doGet(t, app, h)
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode, "header %q", h)
		_ = resp.Body.Close()
	}
}

func TestMiddleware_ValidTokenPopulatesIdentityAndGroups(t *testing.T) {
	tk := newTestTokens(t)
	uid := uuid.New()
	gid := uuid.New()
	g := &fakeGroups{ids: []pgtype.UUID{{Bytes: gid, Valid: true}}}
	app := newTestApp(NewMiddleware(tk, g, discardLogger()))

	raw, _, err := tk.Issue(uid.String(), "user@example.com")
	require.NoError(t, err)
	resp := doGet(t, app, "Bearer "+raw)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, pgtype.UUID{Bytes: uid, Valid: true}, g.gotID)

	var got Identity
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Equal(t, uid.String(), got.UserID)
	require.Equal(t, "user@example.com", got.Email)
	require.Equal(t, []string{gid.String()}, got.Groups)
}

func TestMiddleware_ForeignSecretIs401(t *testing.T) {
	g := &fakeGroups{}
	app := newTestApp(NewMiddleware(newTestTokens(t), g, discardLogger()))

	other, err := NewTokens(strings.Repeat("x", MinSecretBytes), time.Hour)
	require.NoError(t, err)
	raw, _, err := other.Issue(uuid.NewString(), "user@example.com")
	require.NoError(t, err)

	resp := doGet(t, app, "Bearer "+raw)
	defer resp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.False(t, g.called, "an unverified token must never reach the DB")
}

func TestMiddleware_GroupLookupFailureIs500(t *testing.T) {
	tk := newTestTokens(t)
	app := newTestApp(NewMiddleware(tk, &fakeGroups{err: errors.New("db down")}, discardLogger()))

	raw, _, err := tk.Issue(uuid.NewString(), "user@example.com")
	require.NoError(t, err)
	resp := doGet(t, app, "Bearer "+raw)
	defer resp.Body.Close()
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestMiddleware_NilGroupsYieldsEmptyGroups(t *testing.T) {
	tk := newTestTokens(t)
	app := newTestApp(NewMiddleware(tk, nil, discardLogger()))

	raw, _, err := tk.Issue(uuid.NewString(), "user@example.com")
	require.NoError(t, err)
	resp := doGet(t, app, "Bearer "+raw)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var got Identity
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Empty(t, got.Groups)
}
