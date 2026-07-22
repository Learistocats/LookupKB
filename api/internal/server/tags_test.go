package server

import (
	"context"
	"crypto"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/Learistocats/LookupKB/api/internal/config"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/stretchr/testify/require"
)

// fakeTagStore records the last search args and returns canned rows/errors, so the
// autocomplete handler is tested without a database.
type fakeTagStore struct {
	gotQuery string
	gotLimit int32
	rows     []dbgen.Tag
	err      error
}

func (f *fakeTagStore) SearchTags(_ context.Context, query string, limit int32) ([]dbgen.Tag, error) {
	f.gotQuery = query
	f.gotLimit = limit
	return f.rows, f.err
}

// newTagServer wires a real auth middleware (static-key verifier) plus the given
// tag store, mirroring newDraftServer so requests exercise the authed route path.
func newTagServer(t *testing.T, store TagStore) *Server {
	t.Helper()
	cfg, err := config.Load()
	require.NoError(t, err)

	ks := &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{testKey.Public()}}
	verifier := auth.VerifierFrom(oidc.NewVerifier(testIssuer, ks, &oidc.Config{ClientID: testAudience}))
	up := &fakeUpserter{userID: pgUUID(callerUUID)}
	mw := auth.NewMiddleware(verifier, up, slogDiscard())

	return New(cfg, slogDiscard(), "test", WithAuth(mw), WithTags(store))
}

func TestListTags_ReturnsSuggestionsWithCounts(t *testing.T) {
	store := &fakeTagStore{rows: []dbgen.Tag{
		{Slug: "api", Label: "api", UsageCount: 9},
		{Slug: "api-gateway", Label: "api-gateway", UsageCount: 2},
	}}
	s := newTagServer(t, store)

	resp := doReq(t, s, "GET", "/api/v1/tags?q=api", "")
	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)

	var got []tagResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Len(t, got, 2)
	require.Equal(t, "api", got[0].Slug)
	require.Equal(t, int32(9), got[0].UsageCount)
	require.Equal(t, "api", store.gotQuery, "raw query forwarded to the store")
}

func TestListTags_EmptyQueryAllowed(t *testing.T) {
	store := &fakeTagStore{rows: []dbgen.Tag{{Slug: "popular", Label: "popular", UsageCount: 5}}}
	s := newTagServer(t, store)

	resp := doReq(t, s, "GET", "/api/v1/tags", "")
	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)
	require.Equal(t, "", store.gotQuery)
}

func TestListTags_ForwardsLimit(t *testing.T) {
	store := &fakeTagStore{}
	s := newTagServer(t, store)

	resp := doReq(t, s, "GET", "/api/v1/tags?q=a&limit=7", "")
	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)
	require.Equal(t, int32(7), store.gotLimit)
}

func TestListTags_BadLimitIgnored(t *testing.T) {
	store := &fakeTagStore{}
	s := newTagServer(t, store)

	// A non-numeric limit falls back to 0 (store applies its default) rather than 400.
	resp := doReq(t, s, "GET", "/api/v1/tags?q=a&limit=abc", "")
	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)
	require.Equal(t, int32(0), store.gotLimit)
}

func TestListTags_RequiresAuth(t *testing.T) {
	store := &fakeTagStore{}
	s := newTagServer(t, store)

	// No bearer → the auth middleware rejects before the handler runs.
	req, _ := http.NewRequest("GET", "/api/v1/tags?q=api", nil)
	resp, err := s.App().Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 401, resp.StatusCode)
}
