package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Learistocats/LookupKB/api/internal/access"
	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/Learistocats/LookupKB/api/internal/config"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/stretchr/testify/require"
)

// fakeTagStore records the last search args and returns canned rows/errors, so the
// autocomplete handler is tested without a database.
type fakeTagStore struct {
	gotQuery  string
	gotLimit  int32
	gotCaller access.Caller
	rows      []dbgen.SearchTagsRow
	err       error
}

func (f *fakeTagStore) SearchTags(_ context.Context, query string, limit int32, caller access.Caller) ([]dbgen.SearchTagsRow, error) {
	f.gotQuery = query
	f.gotLimit = limit
	f.gotCaller = caller
	return f.rows, f.err
}

// newTagServer wires a real auth middleware (static-key verifier) plus the given
// tag store, mirroring newDraftServer so requests exercise the authed route path.
func newTagServer(t *testing.T, store TagStore) *Server {
	t.Helper()
	cfg, err := config.Load()
	require.NoError(t, err)

	mw := auth.NewMiddleware(testTokens, nil, slogDiscard())

	return New(cfg, slogDiscard(), "test", WithAuth(mw), WithTags(store))
}

func TestListTags_ReturnsSuggestionsWithCounts(t *testing.T) {
	store := &fakeTagStore{rows: []dbgen.SearchTagsRow{
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
	store := &fakeTagStore{rows: []dbgen.SearchTagsRow{{Slug: "popular", Label: "popular", UsageCount: 5}}}
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

func TestListTags_BadLimitIs400(t *testing.T) {
	s := newTagServer(t, &fakeTagStore{})

	resp := doReq(t, s, "GET", "/api/v1/tags?q=a&limit=abc", "")
	defer resp.Body.Close()
	require.Equal(t, 400, resp.StatusCode)
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

// LOO-33: the store gets the verified caller (to hide protected tags they lack)
// and each suggestion carries the protected flag for the publish dialog.
func TestListTags_ForwardsCallerAndProtectedFlag(t *testing.T) {
	store := &fakeTagStore{rows: []dbgen.SearchTagsRow{
		{Slug: "reeflect", Label: "reeflect", UsageCount: 1, IsProtected: true},
		{Slug: "go", Label: "go", UsageCount: 3},
	}}
	s := newTagServer(t, store)

	resp := doReq(t, s, "GET", "/api/v1/tags?q=e", "")
	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)
	require.Equal(t, pgUUID(callerUUID), store.gotCaller.UserID)
	require.False(t, store.gotCaller.IsSuperAdmin)

	var got []tagResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.True(t, got[0].Protected)
	require.False(t, got[1].Protected)
}

func TestListTags_SuperAdminCallerForwarded(t *testing.T) {
	store := &fakeTagStore{}
	cfg, err := config.Load()
	require.NoError(t, err)
	s := New(cfg, slogDiscard(), "test",
		WithAuth(auth.NewMiddleware(testTokens, &fakeDirectory{super: true}, slogDiscard())),
		WithTags(store))
	resp := doReq(t, s, "GET", "/api/v1/tags?q=e", "")
	_ = resp.Body.Close()
	require.True(t, store.gotCaller.IsSuperAdmin)
}
