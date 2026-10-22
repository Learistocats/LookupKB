package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/Learistocats/LookupKB/api/internal/config"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

// fakeUserStore is an in-memory users table keyed by lower-cased email (mirrors
// the citext UNIQUE column).
type fakeUserStore struct {
	byEmail map[string]dbgen.User
	touched bool
}

func newFakeUserStore() *fakeUserStore {
	return &fakeUserStore{byEmail: map[string]dbgen.User{}}
}

func (f *fakeUserStore) CreateUser(_ context.Context, arg dbgen.CreateUserParams) (dbgen.User, error) {
	k := strings.ToLower(arg.Email)
	if _, ok := f.byEmail[k]; ok {
		return dbgen.User{}, &pgconn.PgError{Code: "23505"}
	}
	u := dbgen.User{ID: pgUUID(callerUUID), Email: arg.Email, PasswordHash: arg.PasswordHash, DisplayName: arg.DisplayName}
	f.byEmail[k] = u
	return u, nil
}

func (f *fakeUserStore) GetUserByEmail(_ context.Context, email string) (dbgen.User, error) {
	u, ok := f.byEmail[strings.ToLower(email)]
	if !ok {
		return dbgen.User{}, pgx.ErrNoRows
	}
	return u, nil
}

func (f *fakeUserStore) ListUserGroupIDs(context.Context, pgtype.UUID) ([]pgtype.UUID, error) {
	return nil, nil
}

func (f *fakeUserStore) TouchUserLastSeen(_ context.Context, _ pgtype.UUID) error {
	f.touched = true
	return nil
}

func newAccountServer(t *testing.T, users UserStore) *Server {
	t.Helper()
	cfg, err := config.Load()
	require.NoError(t, err)
	return New(cfg, slogDiscard(), "test",
		WithAuth(auth.NewMiddleware(testTokens, nil, slogDiscard())),
		WithAccounts(users, testTokens),
	)
}

func postJSON(t *testing.T, s *Server, target, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.App().Test(req, -1)
	require.NoError(t, err)
	return resp
}

func TestSignup_CreatesAccountAndReturnsVerifiableToken(t *testing.T) {
	users := newFakeUserStore()
	s := newAccountServer(t, users)

	resp := postJSON(t, s, "/api/v1/auth/signup", `{"email":"Ada@example.com","password":"a long enough password","display_name":"Ada"}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var got sessionResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Equal(t, "Ada@example.com", got.User.Email)
	require.Equal(t, callerUUID.String(), got.User.ID)
	require.NotEmpty(t, got.ExpiresAt)

	claims, err := testTokens.Verify(got.Token)
	require.NoError(t, err)
	require.Equal(t, callerUUID.String(), claims.UserID)

	stored := users.byEmail["ada@example.com"]
	require.True(t, auth.VerifyPassword(stored.PasswordHash, "a long enough password"), "password stored as argon2id hash")
	require.NotContains(t, stored.PasswordHash, "a long enough password")
}

func TestSignup_RejectsInvalidInput(t *testing.T) {
	s := newAccountServer(t, newFakeUserStore())
	for _, body := range []string{
		`{"email":"not-an-email","password":"a long enough password"}`,
		`{"email":"Ada <ada@example.com>","password":"a long enough password"}`,
		`{"email":"ada@example.com","password":"short"}`,
	} {
		resp := postJSON(t, s, "/api/v1/auth/signup", body)
		require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode, body)
		_ = resp.Body.Close()
	}
}

func TestSignup_DuplicateEmailIs409(t *testing.T) {
	s := newAccountServer(t, newFakeUserStore())
	resp := postJSON(t, s, "/api/v1/auth/signup", `{"email":"ada@example.com","password":"a long enough password"}`)
	_ = resp.Body.Close()

	resp = postJSON(t, s, "/api/v1/auth/signup", `{"email":"ADA@example.com","password":"another long password"}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestLogin_SucceedsWithCorrectPassword(t *testing.T) {
	users := newFakeUserStore()
	s := newAccountServer(t, users)
	resp := postJSON(t, s, "/api/v1/auth/signup", `{"email":"ada@example.com","password":"a long enough password"}`)
	_ = resp.Body.Close()

	resp = postJSON(t, s, "/api/v1/auth/login", `{"email":"ada@example.com","password":"a long enough password"}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.True(t, users.touched)

	var got sessionResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	_, err := testTokens.Verify(got.Token)
	require.NoError(t, err)
}

// Unknown email and wrong password must be indistinguishable to the caller.
func TestLogin_FailuresAreIdentical(t *testing.T) {
	s := newAccountServer(t, newFakeUserStore())
	resp := postJSON(t, s, "/api/v1/auth/signup", `{"email":"ada@example.com","password":"a long enough password"}`)
	_ = resp.Body.Close()

	read := func(body string) (int, string) {
		r := postJSON(t, s, "/api/v1/auth/login", body)
		defer r.Body.Close()
		var m map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&m))
		return r.StatusCode, m["error"]
	}
	wrongStatus, wrongMsg := read(`{"email":"ada@example.com","password":"the wrong password!"}`)
	unknownStatus, unknownMsg := read(`{"email":"nobody@example.com","password":"the wrong password!"}`)

	require.Equal(t, http.StatusUnauthorized, wrongStatus)
	require.Equal(t, wrongStatus, unknownStatus)
	require.Equal(t, wrongMsg, unknownMsg)
}

func TestAccounts_RateLimited(t *testing.T) {
	s := newAccountServer(t, newFakeUserStore())
	var last int
	for i := 0; i < 11; i++ {
		resp := postJSON(t, s, "/api/v1/auth/login", `{"email":"x@example.com","password":"whatever-password"}`)
		last = resp.StatusCode
		_ = resp.Body.Close()
	}
	require.Equal(t, http.StatusTooManyRequests, last)
}

// Without a user store the account routes do not exist (deny-by-default).
func TestAccounts_NotRegisteredWithoutStore(t *testing.T) {
	cfg, err := config.Load()
	require.NoError(t, err)
	s := New(cfg, slogDiscard(), "test", WithAuth(auth.NewMiddleware(testTokens, nil, slogDiscard())))

	resp := postJSON(t, s, "/api/v1/auth/login", `{"email":"x@example.com","password":"whatever-password"}`)
	defer resp.Body.Close()
	// Falls through to the protected /api group: no token ⇒ 401, never a login.
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}
