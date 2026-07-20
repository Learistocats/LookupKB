package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

// fakeUpserter records the upsert call and returns a canned row/error.
type fakeUpserter struct {
	called bool
	gotSub string
	gotErr error
	user   dbgen.User
}

func (f *fakeUpserter) UpsertUser(_ context.Context, arg dbgen.UpsertUserParams) (dbgen.User, error) {
	f.called = true
	f.gotSub = arg.OidcSub
	if f.gotErr != nil {
		return dbgen.User{}, f.gotErr
	}
	return f.user, nil
}

func testUserRow() dbgen.User {
	id := uuid.New()
	return dbgen.User{ID: pgtype.UUID{Bytes: id, Valid: true}}
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
	v := newTestVerifier(t, testIssuer, testAudience, testKey.Public())
	up := &fakeUpserter{user: testUserRow()}
	app := newTestApp(NewMiddleware(v, up, discardLogger()))

	resp := doGet(t, app, "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.False(t, up.called, "no token must not touch the database")
}

func TestMiddleware_MalformedHeaderIs401(t *testing.T) {
	v := newTestVerifier(t, testIssuer, testAudience, testKey.Public())
	up := &fakeUpserter{user: testUserRow()}
	app := newTestApp(NewMiddleware(v, up, discardLogger()))

	for _, h := range []string{"Basic abc", "Bearer", "Bearer ", "token xyz"} {
		resp := doGet(t, app, h)
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode, "header %q", h)
		_ = resp.Body.Close()
	}
}

func TestMiddleware_ValidTokenPopulatesIdentityAndUpserts(t *testing.T) {
	v := newTestVerifier(t, testIssuer, testAudience, testKey.Public())
	row := testUserRow()
	up := &fakeUpserter{user: row}
	app := newTestApp(NewMiddleware(v, up, discardLogger()))

	raw := signToken(t, testKey, validClaims())
	resp := doGet(t, app, "Bearer "+raw)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.True(t, up.called)
	require.Equal(t, "ak-sub-123", up.gotSub)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var got Identity
	require.NoError(t, json.Unmarshal(body, &got))
	require.Equal(t, "ak-sub-123", got.Sub)
	require.Equal(t, "user@example.com", got.Email)
	require.Equal(t, []string{"security", "hr"}, got.Groups)
	require.Equal(t, uuidString(row.ID), got.UserID)
}

func TestMiddleware_InvalidTokenIs401AndSkipsUpsert(t *testing.T) {
	v := newTestVerifier(t, testIssuer, testAudience, testKey.Public())
	up := &fakeUpserter{user: testUserRow()}
	app := newTestApp(NewMiddleware(v, up, discardLogger()))

	claims := validClaims()
	claims["aud"] = "other-app"
	raw := signToken(t, testKey, claims)

	resp := doGet(t, app, "Bearer "+raw)
	defer resp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.False(t, up.called, "an unverified token must never reach the DB")
}

func TestMiddleware_UpsertFailureIs500(t *testing.T) {
	v := newTestVerifier(t, testIssuer, testAudience, testKey.Public())
	up := &fakeUpserter{gotErr: errors.New("db down")}
	app := newTestApp(NewMiddleware(v, up, discardLogger()))

	raw := signToken(t, testKey, validClaims())
	resp := doGet(t, app, "Bearer "+raw)
	defer resp.Body.Close()
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestMiddleware_NilUpserterLeavesUserIDEmpty(t *testing.T) {
	v := newTestVerifier(t, testIssuer, testAudience, testKey.Public())
	app := newTestApp(NewMiddleware(v, nil, discardLogger()))

	raw := signToken(t, testKey, validClaims())
	resp := doGet(t, app, "Bearer "+raw)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	var got Identity
	require.NoError(t, json.Unmarshal(body, &got))
	require.Empty(t, got.UserID)
	require.Equal(t, "ak-sub-123", got.Sub)
}
