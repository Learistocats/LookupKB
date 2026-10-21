package server

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/Learistocats/LookupKB/api/internal/config"
	"github.com/Learistocats/LookupKB/api/internal/drafts"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

// fakeDraftDeleter records its inputs and returns a canned error.
type fakeDraftDeleter struct {
	err      error
	called   bool
	docID    pgtype.UUID
	authorID pgtype.UUID
}

func (f *fakeDraftDeleter) Delete(_ context.Context, docID, authorID pgtype.UUID) error {
	f.called = true
	f.docID, f.authorID = docID, authorID
	return f.err
}

func newDeleteServer(t *testing.T, store DocumentStore, del DraftDeleter) *Server {
	t.Helper()
	cfg, err := config.Load()
	require.NoError(t, err)
	opts := []Option{WithAuth(auth.NewMiddleware(testTokens, nil, slogDiscard()))}
	if store != nil {
		opts = append(opts, WithDocuments(store))
	}
	if del != nil {
		opts = append(opts, WithDraftDelete(del))
	}
	return New(cfg, slogDiscard(), "test", opts...)
}

func TestDeleteDraft_Returns204ScopedToCaller(t *testing.T) {
	id := uuid.New()
	del := &fakeDraftDeleter{}
	s := newDeleteServer(t, &fakeDocStore{}, del)

	resp := doReq(t, s, http.MethodDelete, "/api/drafts/"+id.String(), "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Equal(t, pgUUID(id), del.docID)
	require.Equal(t, pgUUID(callerUUID), del.authorID)
}

// Missing, another author's and published documents all surface from the
// service as ErrNotFound; the 404 body must be byte-identical to the one GET
// returns for a missing draft, so nothing about existence leaks.
func TestDeleteDraft_NotFoundIsIdentical404(t *testing.T) {
	s := newDeleteServer(t, &fakeDocStore{getErr: pgx.ErrNoRows}, &fakeDraftDeleter{err: drafts.ErrNotFound})
	id := "/api/drafts/" + uuid.NewString()

	delResp := doReq(t, s, http.MethodDelete, id, "")
	defer delResp.Body.Close()
	getResp := doReq(t, s, http.MethodGet, id, "")
	defer getResp.Body.Close()

	require.Equal(t, http.StatusNotFound, delResp.StatusCode)
	delBody, _ := io.ReadAll(delResp.Body)
	getBody, _ := io.ReadAll(getResp.Body)
	require.Equal(t, string(getBody), string(delBody))
}

func TestDeleteDraft_MalformedIDIs400(t *testing.T) {
	del := &fakeDraftDeleter{}
	s := newDeleteServer(t, &fakeDocStore{}, del)

	resp := doReq(t, s, http.MethodDelete, "/api/drafts/not-a-uuid", "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.False(t, del.called)
}

func TestDeleteDraft_ServiceErrorIs500(t *testing.T) {
	s := newDeleteServer(t, &fakeDocStore{}, &fakeDraftDeleter{err: context.DeadlineExceeded})

	resp := doReq(t, s, http.MethodDelete, "/api/drafts/"+uuid.NewString(), "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// Deny-by-default: no delete service, or no documents store, ⇒ no DELETE route.
func TestDeleteDraft_UnregisteredWithoutDependencies(t *testing.T) {
	for name, s := range map[string]*Server{
		"no service": newDeleteServer(t, &fakeDocStore{}, nil),
		"no store":   newDeleteServer(t, nil, &fakeDraftDeleter{}),
	} {
		resp := doReq(t, s, http.MethodDelete, "/api/drafts/"+uuid.NewString(), "")
		resp.Body.Close()
		require.NotEqual(t, http.StatusNoContent, resp.StatusCode, name)
		require.Contains(t, []int{http.StatusNotFound, http.StatusMethodNotAllowed}, resp.StatusCode, name)
	}
}

func TestDeleteDraft_RequiresAuth(t *testing.T) {
	del := &fakeDraftDeleter{}
	s := newDeleteServer(t, &fakeDocStore{}, del)

	req, _ := http.NewRequest(http.MethodDelete, "/api/drafts/"+uuid.NewString(), nil)
	resp, err := s.App().Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.False(t, del.called)
}
