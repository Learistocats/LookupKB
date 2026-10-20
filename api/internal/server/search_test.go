package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

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
	caller search.Caller
	res    search.Result
	err    error
}

func (f *fakeSearch) Search(_ context.Context, q string, limit int, caller search.Caller) (search.Result, error) {
	f.q, f.limit, f.caller = q, limit, caller
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
	require.Equal(t, callerUUID.String(), svc.caller.UserID, "the filter identity comes from the token, never the query")

	var got search.Result
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Len(t, got.Hits, 1)
}

func TestSearch_TruncatesLongQueries(t *testing.T) {
	svc := &fakeSearch{}
	s := newSearchServer(t, svc)
	resp := doReq(t, s, http.MethodGet, "/api/search?q="+strings.Repeat("a", 500), "")
	_ = resp.Body.Close()
	require.Len(t, svc.q, maxQueryRunes)
}

func TestSearch_EngineDownIs503(t *testing.T) {
	s := newSearchServer(t, &fakeSearch{err: errors.New("meili down")})
	resp := doReq(t, s, http.MethodGet, "/api/search?q=x", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
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
