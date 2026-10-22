package auth

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func TestGeneratePAT_FormatAndHash(t *testing.T) {
	p, err := GeneratePAT()
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(p.Token, "lkb_pat_"+p.Prefix+"_"))
	require.Len(t, p.Prefix, PATPrefixLen)
	require.Len(t, p.Token, len(PATScheme)+PATPrefixLen+1+patSecretLen)
	require.Equal(t, HashPAT(p.Token), p.Hash)
	require.Len(t, p.Hash, 64)
	require.NotContains(t, p.Hash, p.Token)

	prefix, ok := ParsePAT(p.Token)
	require.True(t, ok)
	require.Equal(t, p.Prefix, prefix)

	q, err := GeneratePAT()
	require.NoError(t, err)
	require.NotEqual(t, p.Token, q.Token)
	require.NotEqual(t, p.Prefix, q.Prefix)
}

func TestParsePAT_RejectsMalformed(t *testing.T) {
	good, err := GeneratePAT()
	require.NoError(t, err)
	for _, raw := range []string{
		"",
		"lkb_pat_",
		"lkb_pat_short_x",
		strings.Replace(good.Token, "_"+good.Prefix+"_", "_"+good.Prefix+"-", 1),
		good.Token + "x",
		good.Token[:len(good.Token)-1] + "!",
		"lkb_pat_" + "ab$defgh" + good.Token[len(PATScheme)+PATPrefixLen:],
	} {
		_, ok := ParsePAT(raw)
		require.False(t, ok, "%q", raw)
	}
}

func TestPATAllowed(t *testing.T) {
	cases := []struct {
		method, path string
		want         bool
	}{
		{http.MethodGet, "/api/v1/me", true},
		{http.MethodGet, "/api/v1/tags", true},
		{http.MethodGet, "/API/V1/ME", true},
		{http.MethodHead, "/api/v1/tags", true},
		{http.MethodGet, "/api/v1", true},
		{http.MethodPost, "/api/v1/tags", false},
		{http.MethodDelete, "/api/v1/me", false},
		{http.MethodGet, "/api/v1/auth/login", false},
		{http.MethodPost, "/api/v1/auth/login", false},
		{http.MethodGet, "/api/v1x/me", false},
		{http.MethodGet, "/api/v1/../drafts", false},
		{http.MethodGet, "/api/drafts", false},
		{http.MethodGet, "/api/documents/mine", false},
		{http.MethodGet, "/api/tokens", false},
		{http.MethodPost, "/api/tokens", false},
		{http.MethodPost, "/api/documents", false},
		{http.MethodGet, "/api/search", false},
		{http.MethodPost, "/mcp", true},
		{http.MethodGet, "/mcp", true},
		{http.MethodDelete, "/mcp", false},
		{http.MethodPost, "/mcpx", false},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, PATAllowed(tc.method, tc.path), "%s %s", tc.method, tc.path)
	}
}

func issuePAT(t *testing.T, f *fakeStore, mutate func(*dbgen.GetActiveAPITokenByPrefixRow)) (string, dbgen.GetActiveAPITokenByPrefixRow) {
	t.Helper()
	p, err := GeneratePAT()
	require.NoError(t, err)
	row := dbgen.GetActiveAPITokenByPrefixRow{
		ID:        pgtype.UUID{Bytes: uuid.New(), Valid: true},
		UserID:    pgtype.UUID{Bytes: uuid.New(), Valid: true},
		Email:     "pat@example.com",
		TokenHash: p.Hash,
	}
	if mutate != nil {
		mutate(&row)
	}
	if f.rows == nil {
		f.rows = map[string]dbgen.GetActiveAPITokenByPrefixRow{}
	}
	f.rows[p.Prefix] = row
	return p.Token, row
}

// newPATApp mounts the middleware the way the server does (a prefix group) with a
// read route and a write route so the surface restriction is exercised.
func newPATApp(mw *Middleware) *fiber.App {
	app := fiber.New()
	echo := func(c *fiber.Ctx) error {
		id, ok := IdentityFromCtx(c)
		if !ok {
			return c.SendStatus(fiber.StatusInternalServerError)
		}
		return c.JSON(id)
	}
	api := app.Group("/api", mw.Handle)
	api.Get("/v1/me", echo)
	api.Post("/documents", echo)
	api.Get("/drafts", echo)
	api.Get("/tokens", echo)
	return app
}

func doPAT(t *testing.T, app *fiber.App, method, path, authHeader string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, path, nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(b)
}

func TestMiddleware_ValidPATActsAsUser(t *testing.T) {
	st := &fakeStore{}
	raw, row := issuePAT(t, st, func(r *dbgen.GetActiveAPITokenByPrefixRow) { r.IsSuperAdmin = true })
	app := newPATApp(NewMiddleware(newTestTokens(t), st, discardLogger()))

	resp := doPAT(t, app, http.MethodGet, "/api/v1/me", "Bearer "+raw)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var got Identity
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Equal(t, uuid.UUID(row.UserID.Bytes), got.UserID)
	require.Equal(t, "pat@example.com", got.Email)
	require.Equal(t, MethodPAT, got.Method)
	require.Equal(t, uuid.UUID(row.ID.Bytes).String(), got.TokenID)
	require.True(t, got.IsSuperAdmin)
	require.Zero(t, st.superHits, "the PAT lookup already carries the flag")
	require.Equal(t, []pgtype.UUID{row.ID}, st.touched)
}

func TestMiddleware_PATLastUsedIsThrottled(t *testing.T) {
	st := &fakeStore{}
	raw, _ := issuePAT(t, st, func(r *dbgen.GetActiveAPITokenByPrefixRow) {
		r.LastUsedAt = pgtype.Timestamptz{Time: time.Now().Add(-10 * time.Second), Valid: true}
	})
	app := newPATApp(NewMiddleware(newTestTokens(t), st, discardLogger()))

	resp := doPAT(t, app, http.MethodGet, "/api/v1/me", "Bearer "+raw)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Empty(t, st.touched, "a token used within the last minute must not be re-stamped")
}

// Every PAT failure answers exactly what a bad JWT answers.
func TestMiddleware_InvalidPATsAre401LikeBadJWT(t *testing.T) {
	st := &fakeStore{}
	app := newPATApp(NewMiddleware(newTestTokens(t), st, discardLogger()))

	jwtResp := doPAT(t, app, http.MethodGet, "/api/v1/me", "Bearer not.a.jwt")
	require.Equal(t, http.StatusUnauthorized, jwtResp.StatusCode)
	want := readBody(t, jwtResp)

	expired, _ := issuePAT(t, st, func(r *dbgen.GetActiveAPITokenByPrefixRow) {
		r.ExpiresAt = pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true}
	})
	valid, _ := issuePAT(t, st, nil)
	// Same prefix, different secret.
	wrongSecret := valid[:len(valid)-4] + "AAAA"
	if wrongSecret == valid {
		wrongSecret = valid[:len(valid)-4] + "BBBB"
	}
	unknown, err := GeneratePAT()
	require.NoError(t, err)

	for name, raw := range map[string]string{
		"expired":       expired,
		"wrong secret":  wrongSecret,
		"unknown":       unknown.Token,
		"malformed":     "lkb_pat_nope",
		"empty pat":     "lkb_pat_",
		"case mismatch": strings.ToUpper(valid[:8]) + valid[8:],
	} {
		resp := doPAT(t, app, http.MethodGet, "/api/v1/me", "Bearer "+raw)
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode, name)
		require.Equal(t, want, readBody(t, resp), name)
	}
	require.Empty(t, st.touched, "a rejected token is never stamped")
}

func TestMiddleware_PATWithoutStoreIs401(t *testing.T) {
	raw, _ := issuePAT(t, &fakeStore{}, nil)
	app := newPATApp(NewMiddleware(newTestTokens(t), nil, discardLogger()))

	resp := doPAT(t, app, http.MethodGet, "/api/v1/me", "Bearer "+raw)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestMiddleware_PATStoreErrorIs500(t *testing.T) {
	st := &fakeStore{lookupErr: errors.New("db down")}
	p, err := GeneratePAT()
	require.NoError(t, err)
	app := newPATApp(NewMiddleware(newTestTokens(t), st, discardLogger()))

	resp := doPAT(t, app, http.MethodGet, "/api/v1/me", "Bearer "+p.Token)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestMiddleware_PATBlockedOffReadSurface(t *testing.T) {
	st := &fakeStore{}
	raw, _ := issuePAT(t, st, nil)
	app := newPATApp(NewMiddleware(newTestTokens(t), st, discardLogger()))

	for _, r := range []struct{ method, path string }{
		{http.MethodPost, "/api/documents"},
		{http.MethodGet, "/api/drafts"},
		{http.MethodGet, "/api/tokens"},
		{http.MethodGet, "/API/DRAFTS"},
	} {
		resp := doPAT(t, app, r.method, r.path, "Bearer "+raw)
		_ = resp.Body.Close()
		require.Equal(t, http.StatusForbidden, resp.StatusCode, "%s %s", r.method, r.path)
	}
	require.Empty(t, st.touched, "a blocked PAT is never stamped")
}

func TestMiddleware_JWTUnchangedAndReachesWriteRoutes(t *testing.T) {
	tk := newTestTokens(t)
	app := newPATApp(NewMiddleware(tk, &fakeStore{}, discardLogger()))
	uid := uuid.New()
	raw, _, err := tk.Issue(uid.String(), "user@example.com")
	require.NoError(t, err)

	resp := doPAT(t, app, http.MethodPost, "/api/documents", "Bearer "+raw)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var got Identity
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Equal(t, uid, got.UserID)
	require.Equal(t, MethodSession, got.Method)
	require.Empty(t, got.TokenID)
}
