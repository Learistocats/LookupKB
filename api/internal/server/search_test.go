package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/Learistocats/LookupKB/api/internal/access"
	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/Learistocats/LookupKB/api/internal/config"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/Learistocats/LookupKB/api/internal/search"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type fakeSearch struct {
	q      string
	limit  int
	query  search.Query
	caller access.Caller
	res    search.Result
	err    error
}

func (f *fakeSearch) Search(_ context.Context, q search.Query, caller access.Caller) (search.Result, error) {
	f.q, f.limit, f.query, f.caller = q.Q, q.Limit, q, caller
	return f.res, f.err
}

func newSearchServer(t *testing.T, svc SearchService) *Server {
	t.Helper()
	cfg, err := config.Load()
	require.NoError(t, err)
	return New(cfg, slogDiscard(), "test",
		WithAuth(auth.NewMiddleware(testTokens, nil, slogDiscard())),
		WithSearch(svc))
}

func TestSearch_ForwardsQueryAndVerifiedCaller(t *testing.T) {
	svc := &fakeSearch{res: search.Result{Hits: []search.Hit{{Slug: "a-doc", Title: "A doc"}}}}
	s := newSearchServer(t, svc)

	resp := doReq(t, s, http.MethodGet, "/api/search?q=kube&limit=5", "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "kube", svc.q)
	require.Equal(t, 5, svc.limit)
	require.Equal(t, pgUUID(callerUUID), svc.caller.UserID, "the filter identity comes from the token, never the query")
	require.False(t, svc.caller.IsSuperAdmin)

	var got search.Result
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Len(t, got.Hits, 1)
}

func TestSearch_BadLimitOrOffsetIs400(t *testing.T) {
	s := newSearchServer(t, &fakeSearch{})
	for _, q := range []string{"limit=abc", "limit=-1", "offset=x", "offset=-5"} {
		resp := doReq(t, s, http.MethodGet, "/api/search?q=x&"+q, "")
		_ = resp.Body.Close()
		require.Equal(t, http.StatusBadRequest, resp.StatusCode, q)
	}
}

func TestSearch_EngineDownIs503(t *testing.T) {
	s := newSearchServer(t, &fakeSearch{err: errors.New("meili down")})
	resp := doReq(t, s, http.MethodGet, "/api/search?q=x", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

// LOO-34: allowlisted client filters and facets are parsed and forwarded; the
// caller identity still comes only from the token (a client author_id is a
// narrowing filter, never the access identity).
func TestSearch_ForwardsFiltersAndFacets(t *testing.T) {
	svc := &fakeSearch{res: search.Result{Hits: []search.Hit{}, Facets: map[string]map[string]int64{"tags": {"go": 2}}}}
	s := newSearchServer(t, svc)
	other := uuid.NewString()

	resp := doReq(t, s, http.MethodGet, "/api/search?q=x&facets=tags&tags=go,ops&tags=k8s&author_id="+other+
		"&published_from=2026-01-01&published_to=2026-02-01&access=all", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, []string{"tags"}, svc.query.Facets)
	require.Equal(t, search.Filters{
		Tags: []string{"go", "ops", "k8s"}, AuthorID: other,
		PublishedFrom: "2026-01-01", PublishedTo: "2026-02-01",
	}, svc.query.Filters)
	require.Equal(t, pgUUID(callerUUID), svc.caller.UserID)

	var got map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Contains(t, got, "facets")
}

// Without ?facets the response keeps its pre-LOO-34 shape (no facets key).
func TestSearch_NoFacetsKeyByDefault(t *testing.T) {
	s := newSearchServer(t, &fakeSearch{res: search.Result{Hits: []search.Hit{}}})
	resp := doReq(t, s, http.MethodGet, "/api/search?q=x", "")
	defer resp.Body.Close()
	var got map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.NotContains(t, got, "facets")
	require.Contains(t, got, "hits")
}

func TestSearch_InvalidFilterIs400(t *testing.T) {
	s := newSearchServer(t, &fakeSearch{err: fmt.Errorf("wrap: %w", search.ErrInvalidFilter)})
	resp := doReq(t, s, http.MethodGet, "/api/search?author_id=nope", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestSearch_SuperAdminFlagForwarded(t *testing.T) {
	svc := &fakeSearch{}
	cfg, err := config.Load()
	require.NoError(t, err)
	s := New(cfg, slogDiscard(), "test",
		WithAuth(auth.NewMiddleware(testTokens, &fakeDirectory{super: true}, slogDiscard())),
		WithSearch(svc))
	resp := doReq(t, s, http.MethodGet, "/api/search?q=x", "")
	_ = resp.Body.Close()
	require.True(t, svc.caller.IsSuperAdmin)
}

func TestListMyDocuments_OwnerScopedAndOrderedByStore(t *testing.T) {
	title := "Runbook"
	store := &fakeDocStore{mineRows: []dbgen.ListMyDocumentsRow{
		{ID: pgUUID(uuid.New()), Slug: "draft-1", Status: dbgen.DocumentStatusDraft},
		{ID: pgUUID(uuid.New()), Slug: "runbook-1", Status: dbgen.DocumentStatusPublished, Title: &title},
	}}
	s := newDraftServer(t, store)

	resp := doReq(t, s, http.MethodGet, "/api/documents/mine", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, pgUUID(callerUUID), store.listArg)

	var got []draftListItem
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Len(t, got, 2)
	require.Equal(t, "draft", got[0].Status)
	require.Equal(t, "published", got[1].Status)
}
