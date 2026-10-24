package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/Learistocats/LookupKB/api/internal/config"
	"github.com/Learistocats/LookupKB/api/internal/db"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

// fakeTokenStore is an in-memory TokenStore that also serves as the auth store, so a token minted through POST /api/tokens authenticates for real.
type fakeTokenStore struct {
	rows      map[uuid.UUID]*storedToken
	revokeArg dbgen.RevokeMyAPITokenParams
}

type storedToken struct {
	row  dbgen.CreateAPITokenRow
	hash string
}

func newFakeTokenStore() *fakeTokenStore {
	return &fakeTokenStore{rows: map[uuid.UUID]*storedToken{}}
}

func (f *fakeTokenStore) CreateAPIToken(_ context.Context, arg dbgen.CreateAPITokenParams) (dbgen.CreateAPITokenRow, error) {
	var active int64
	for _, r := range f.rows {
		if r.row.UserID == arg.UserID && !r.row.RevokedAt.Valid {
			active++
		}
	}
	if active >= arg.MaxActive {
		return dbgen.CreateAPITokenRow{}, pgx.ErrNoRows
	}
	id := uuid.New()
	row := dbgen.CreateAPITokenRow{
		ID:        pgUUID(id),
		UserID:    arg.UserID,
		Name:      arg.Name,
		Prefix:    arg.Prefix,
		CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		ExpiresAt: arg.ExpiresAt,
	}
	f.rows[id] = &storedToken{row: row, hash: arg.TokenHash}
	return row, nil
}

func (f *fakeTokenStore) ListMyAPITokens(_ context.Context, userID pgtype.UUID) ([]dbgen.ListMyAPITokensRow, error) {
	var out []dbgen.ListMyAPITokensRow
	for _, r := range f.rows {
		if r.row.UserID != userID {
			continue
		}
		out = append(out, dbgen.ListMyAPITokensRow{
			ID: r.row.ID, Name: r.row.Name, Prefix: r.row.Prefix, CreatedAt: r.row.CreatedAt,
			ExpiresAt: r.row.ExpiresAt, RevokedAt: r.row.RevokedAt,
		})
	}
	return out, nil
}

func (f *fakeTokenStore) RevokeMyAPIToken(_ context.Context, arg dbgen.RevokeMyAPITokenParams) (pgtype.UUID, error) {
	f.revokeArg = arg
	r, ok := f.rows[uuid.UUID(arg.ID.Bytes)]
	if !ok || r.row.UserID != arg.UserID || r.row.RevokedAt.Valid {
		return pgtype.UUID{}, pgx.ErrNoRows
	}
	r.row.RevokedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	return arg.ID, nil
}

// The rest of auth.Store, so a token minted through POST /api/tokens authenticates.
func (f *fakeTokenStore) GetActiveAPITokenByPrefix(_ context.Context, prefix string) (dbgen.GetActiveAPITokenByPrefixRow, error) {
	for _, r := range f.rows {
		if r.row.Prefix == prefix && !r.row.RevokedAt.Valid {
			return dbgen.GetActiveAPITokenByPrefixRow{
				ID: r.row.ID, UserID: r.row.UserID, TokenHash: r.hash, Email: "user@example.com",
			}, nil
		}
	}
	return dbgen.GetActiveAPITokenByPrefixRow{}, pgx.ErrNoRows
}

func (f *fakeTokenStore) TouchAPITokenLastUsed(context.Context, pgtype.UUID) error { return nil }

func (f *fakeTokenStore) GetUserIsSuperAdmin(context.Context, pgtype.UUID) (bool, error) {
	return false, nil
}

func newTokenServer(t *testing.T, store *fakeTokenStore) *Server {
	t.Helper()
	cfg, err := config.Load()
	require.NoError(t, err)
	mw := auth.NewMiddleware(testTokens, store, slogDiscard())
	return New(cfg, slogDiscard(), "test", WithAuth(mw), WithTokens(store), WithDocuments(&fakeDocStore{}))
}

func doWith(t *testing.T, s *Server, method, target, authz, body string) *http.Response {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, target, r)
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.App().Test(req)
	require.NoError(t, err)
	return resp
}

func createToken(t *testing.T, s *Server, body string) tokenResponse {
	t.Helper()
	resp := doReq(t, s, http.MethodPost, "/api/tokens", body)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var got tokenResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	return got
}

func TestCreateToken_ReturnsSecretOnceAndStoresOnlyHash(t *testing.T) {
	store := newFakeTokenStore()
	s := newTokenServer(t, store)

	got := createToken(t, s, `{"name":"  ci  ","expires_in_days":30}`)
	require.Equal(t, "ci", got.Name)
	require.True(t, strings.HasPrefix(got.Token, "lkb_pat_"+got.Prefix+"_"))
	require.NotNil(t, got.ExpiresAt)

	stored := store.rows[uuid.MustParse(got.ID)]
	require.Equal(t, pgUUID(callerUUID), stored.row.UserID)
	require.Equal(t, auth.HashPAT(got.Token), stored.hash)
	require.NotContains(t, stored.hash, got.Token)

	// The list never carries the secret (or the hash).
	resp := doReq(t, s, http.MethodGet, "/api/tokens", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NotContains(t, string(raw), got.Token)
	require.NotContains(t, string(raw), stored.hash)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(raw, &list))
	require.Len(t, list, 1)
	require.Equal(t, got.Prefix, list[0]["prefix"])
	_, hasToken := list[0]["token"]
	require.False(t, hasToken)
}

func TestCreateToken_Validation(t *testing.T) {
	s := newTokenServer(t, newFakeTokenStore())
	for _, body := range []string{
		`{"name":""}`,
		`{"name":"   "}`,
		`{"name":"` + strings.Repeat("x", 101) + `"}`,
		`{"name":"a","expires_in_days":0}`,
		`{"name":"a","expires_in_days":366}`,
		`not json`,
	} {
		resp := doReq(t, s, http.MethodPost, "/api/tokens", body)
		_ = resp.Body.Close()
		require.Equal(t, http.StatusBadRequest, resp.StatusCode, body)
	}
}

func TestCreateToken_CapsActiveTokens(t *testing.T) {
	store := newFakeTokenStore()
	for range maxActiveTokens {
		_, err := store.CreateAPIToken(context.Background(), dbgen.CreateAPITokenParams{
			UserID: pgUUID(callerUUID), Name: "t", MaxActive: maxActiveTokens,
		})
		require.NoError(t, err)
	}
	s := newTokenServer(t, store)
	resp := doReq(t, s, http.MethodPost, "/api/tokens", `{"name":"one too many"}`)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	require.Len(t, store.rows, maxActiveTokens)
}

func TestRevokeToken_OwnAndForeignAndMissing(t *testing.T) {
	store := newFakeTokenStore()
	s := newTokenServer(t, store)

	// A token owned by someone else.
	foreign, err := store.CreateAPIToken(context.Background(), dbgen.CreateAPITokenParams{
		UserID: pgUUID(uuid.New()), Name: "theirs", Prefix: "zzzzzzzz", TokenHash: "h", MaxActive: maxActiveTokens,
	})
	require.NoError(t, err)

	revoke := func(id string) (int, string) {
		resp := doReq(t, s, http.MethodDelete, "/api/tokens/"+id, "")
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	fc, fb := revoke(db.FormatUUID(foreign.ID))
	mc, mb := revoke(uuid.NewString())
	require.Equal(t, http.StatusNotFound, fc)
	require.Equal(t, http.StatusNotFound, mc)
	require.Equal(t, mb, fb, "not-yours and missing must be indistinguishable")
	require.False(t, store.rows[uuid.UUID(foreign.ID.Bytes)].row.RevokedAt.Valid)
	bc, _ := revoke("not-a-uuid")
	require.Equal(t, http.StatusBadRequest, bc)

	mine := createToken(t, s, `{"name":"mine"}`)
	resp := doReq(t, s, http.MethodDelete, "/api/tokens/"+mine.ID, "")
	_ = resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Equal(t, pgUUID(callerUUID), store.revokeArg.UserID)

	// The revoked token no longer authenticates.
	resp = doWith(t, s, http.MethodGet, "/api/v1/me", "Bearer "+mine.Token, "")
	_ = resp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestPAT_ReadSurfaceOnly(t *testing.T) {
	store := newFakeTokenStore()
	s := newTokenServer(t, store)
	tok := createToken(t, s, `{"name":"reader"}`).Token
	authz := "Bearer " + tok

	resp := doWith(t, s, http.MethodGet, "/api/v1/me", authz, "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var me map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&me))
	require.Equal(t, callerUUID.String(), me["id"])

	for _, r := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/tokens", ""},
		{http.MethodPost, "/api/tokens", `{"name":"escalate"}`},
		{http.MethodDelete, "/api/tokens/" + uuid.NewString(), ""},
		{http.MethodPost, "/api/documents", ""},
		{http.MethodGet, "/api/drafts", ""},
	} {
		resp := doWith(t, s, r.method, r.path, authz, r.body)
		_ = resp.Body.Close()
		require.Equal(t, http.StatusForbidden, resp.StatusCode, "%s %s", r.method, r.path)
	}
	require.Len(t, store.rows, 1, "a PAT cannot mint tokens")
}

func TestPAT_RateLimitedPerToken(t *testing.T) {
	store := newFakeTokenStore()
	s := newTokenServer(t, store)
	a := "Bearer " + createToken(t, s, `{"name":"a"}`).Token
	b := "Bearer " + createToken(t, s, `{"name":"b"}`).Token

	for i := 0; i < patRateLimit; i++ {
		resp := doWith(t, s, http.MethodGet, "/api/v1/me", a, "")
		_ = resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode, "request %d", i)
	}
	resp := doWith(t, s, http.MethodGet, "/api/v1/me", a, "")
	_ = resp.Body.Close()
	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	require.NotEmpty(t, resp.Header.Get("Retry-After"))

	// Another token, and the session, have their own budgets.
	resp = doWith(t, s, http.MethodGet, "/api/v1/me", b, "")
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp = doReq(t, s, http.MethodGet, "/api/v1/me", "")
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestTokenRoutesAbsentWithoutStore(t *testing.T) {
	s := newDraftServer(t, nil)
	resp := doReq(t, s, http.MethodGet, "/api/tokens", "")
	_ = resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}
