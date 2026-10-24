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
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

// testTokens signs and verifies the tokens the handler tests send; the real auth
// middleware verifies them, so requests exercise the full authenticated path.
var testTokens = mustTokens()

func mustTokens() *auth.Tokens {
	tk, err := auth.NewTokens("0123456789abcdef0123456789abcdef", time.Hour)
	if err != nil {
		panic(err)
	}
	return tk
}

// fakeDocStore records the params it is called with and returns canned rows, so
// handler behaviour (owner scoping, status codes) is tested without a database.
type fakeDocStore struct {
	createArg dbgen.CreateDraftParams
	saveArg   dbgen.SaveDraftParams
	getArg    dbgen.GetOwnedDocumentParams
	listArg   pgtype.UUID

	createDoc dbgen.Document
	saveDoc   dbgen.Document
	getDoc    dbgen.Document
	listRows  []dbgen.ListDraftsRow

	createErr error
	saveErr   error
	getErr    error
	listErr   error

	mineRows  []dbgen.ListMyDocumentsRow
	tagLabels []string

	getBySlugArg string
	getBySlugRow dbgen.GetPublishedBySlugRow
	getBySlugErr error

	// deniedSlugs are the protected tags the caller lacks (access policy).
	deniedSlugs []string
	deniedCalls int

	listPublishedArg  dbgen.ListPublishedDocumentsParams
	listPublishedRows []dbgen.ListPublishedDocumentsRow
	listPublishedErr  error
}

// ListPublishedDocuments satisfies DocumentStore for the public catalogue tests.
func (f *fakeDocStore) ListPublishedDocuments(_ context.Context, arg dbgen.ListPublishedDocumentsParams) ([]dbgen.ListPublishedDocumentsRow, error) {
	f.listPublishedArg = arg
	rows := f.listPublishedRows
	if int(arg.MaxRows) < len(rows) {
		rows = rows[:arg.MaxRows]
	}
	return rows, f.listPublishedErr
}

func (f *fakeDocStore) CreateDraft(_ context.Context, arg dbgen.CreateDraftParams) (dbgen.Document, error) {
	f.createArg = arg
	return f.createDoc, f.createErr
}

func (f *fakeDocStore) SaveDraft(_ context.Context, arg dbgen.SaveDraftParams) (dbgen.Document, error) {
	f.saveArg = arg
	return f.saveDoc, f.saveErr
}

func (f *fakeDocStore) GetOwnedDocument(_ context.Context, arg dbgen.GetOwnedDocumentParams) (dbgen.Document, error) {
	f.getArg = arg
	return f.getDoc, f.getErr
}

func (f *fakeDocStore) ListDrafts(_ context.Context, authorID pgtype.UUID) ([]dbgen.ListDraftsRow, error) {
	f.listArg = authorID
	return f.listRows, f.listErr
}

func (f *fakeDocStore) ListMyDocuments(_ context.Context, authorID pgtype.UUID) ([]dbgen.ListMyDocumentsRow, error) {
	f.listArg = authorID
	return f.mineRows, f.listErr
}

func (f *fakeDocStore) ListDocumentTagLabels(_ context.Context, _ pgtype.UUID) ([]string, error) {
	return f.tagLabels, nil
}

// GetPublishedBySlug satisfies DocumentStore for the read-path handler tests. It
// records the slug it was asked for and returns the canned row/error.
func (f *fakeDocStore) GetPublishedBySlug(_ context.Context, slug string) (dbgen.GetPublishedBySlugRow, error) {
	f.getBySlugArg = slug
	return f.getBySlugRow, f.getBySlugErr
}

// ListDeniedProtectedTags satisfies access.Querier for the read check.
func (f *fakeDocStore) ListDeniedProtectedTags(_ context.Context, _ pgtype.UUID) ([]dbgen.ListDeniedProtectedTagsRow, error) {
	f.deniedCalls++
	rows := make([]dbgen.ListDeniedProtectedTagsRow, 0, len(f.deniedSlugs))
	for _, s := range f.deniedSlugs {
		rows = append(rows, dbgen.ListDeniedProtectedTagsRow{ID: pgUUID(uuid.New()), Slug: s, Label: s})
	}
	return rows, nil
}

// callerUUID is the internal user id carried by the test token's sub; handlers
// must use it (not the email) as author_id.
var callerUUID = uuid.New()

func pgUUID(u uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: u, Valid: true} }

// draftDoc builds a canned draft row owned by callerUUID.
func draftDoc(id uuid.UUID, source string) dbgen.Document {
	src := source
	return dbgen.Document{
		ID:        pgUUID(id),
		Slug:      "draft-deadbeef",
		AuthorID:  pgUUID(callerUUID),
		Status:    dbgen.DocumentStatusDraft,
		SourceMd:  &src,
		CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		UpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
}

// newDraftServer wires a Server with the real auth middleware and the given documents store, so requests exercise the full
// authenticated route path.
func newDraftServer(t *testing.T, store DocumentStore) *Server {
	t.Helper()
	cfg, err := config.Load()
	require.NoError(t, err)

	mw := auth.NewMiddleware(testTokens, nil, slogDiscard())

	opts := []Option{WithAuth(mw)}
	if store != nil {
		opts = append(opts, WithDocuments(store))
	}
	return New(cfg, slogDiscard(), "test", opts...)
}

func bearer(t *testing.T) string {
	t.Helper()
	raw, _, err := testTokens.Issue(callerUUID.String(), "user@example.com")
	require.NoError(t, err)
	return "Bearer " + raw
}

func doReq(t *testing.T, s *Server, method, target, body string) *http.Response {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, target, r)
	req.Header.Set("Authorization", bearer(t))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.App().Test(req)
	require.NoError(t, err)
	return resp
}

func TestCreateDraft_Returns201OwnedDraft(t *testing.T) {
	id := uuid.New()
	store := &fakeDocStore{createDoc: draftDoc(id, "")}
	s := newDraftServer(t, store)

	resp := doReq(t, s, http.MethodPost, "/api/documents", "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusCreated, resp.StatusCode)
	// author_id must be the caller's internal id, and a placeholder slug is set.
	require.Equal(t, pgUUID(callerUUID), store.createArg.AuthorID)
	require.True(t, strings.HasPrefix(store.createArg.Slug, "draft-"))

	var got draftResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Equal(t, "draft", got.Status)
	require.Equal(t, id.String(), got.ID)
}

func TestSaveDraft_PersistsBodyScopedToCaller(t *testing.T) {
	id := uuid.New()
	store := &fakeDocStore{saveDoc: draftDoc(id, "# hello")}
	s := newDraftServer(t, store)

	resp := doReq(t, s, http.MethodPut, "/api/drafts/"+id.String(), `{"source_md":"# hello"}`)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, pgUUID(id), store.saveArg.ID)
	require.Equal(t, pgUUID(callerUUID), store.saveArg.AuthorID)
	require.NotNil(t, store.saveArg.SourceMd)
	require.Equal(t, "# hello", *store.saveArg.SourceMd)
}

// an empty/partial metadata save must succeed with no validation error.
func TestSaveDraft_EmptyBodySucceeds(t *testing.T) {
	id := uuid.New()
	store := &fakeDocStore{saveDoc: draftDoc(id, "")}
	s := newDraftServer(t, store)

	resp := doReq(t, s, http.MethodPut, "/api/drafts/"+id.String(), `{}`)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Nil(t, store.saveArg.Title)
	require.Nil(t, store.saveArg.SourceMd)
}

func TestGetDraft_ReturnsOwnDraft(t *testing.T) {
	id := uuid.New()
	store := &fakeDocStore{getDoc: draftDoc(id, "# persisted")}
	s := newDraftServer(t, store)

	resp := doReq(t, s, http.MethodGet, "/api/drafts/"+id.String(), "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, pgUUID(callerUUID), store.getArg.AuthorID)

	var got draftResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.NotNil(t, got.SourceMd)
	require.Equal(t, "# persisted", *got.SourceMd)
}

// the workspace list is owner-scoped — the handler must pass the caller's
// internal id as the author filter, and it returns the store's rows projected to
// the light list shape (title-or-null + updated time; never source_md).
func TestListDrafts_ScopedToCaller(t *testing.T) {
	id := uuid.New()
	title := "My draft"
	store := &fakeDocStore{listRows: []dbgen.ListDraftsRow{{
		ID:        pgUUID(id),
		Slug:      "draft-deadbeef",
		Status:    dbgen.DocumentStatusDraft,
		Title:     &title,
		UpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}}}
	s := newDraftServer(t, store)

	resp := doReq(t, s, http.MethodGet, "/api/drafts", "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, pgUUID(callerUUID), store.listArg)

	var got []draftListItem
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Len(t, got, 1)
	require.Equal(t, id.String(), got[0].ID)
	require.NotNil(t, got[0].Title)
	require.Equal(t, "My draft", *got[0].Title)
}

// An empty workspace returns a JSON array ([]), never null, so the client renders
// uniformly.
func TestListDrafts_EmptyReturnsArray(t *testing.T) {
	store := &fakeDocStore{listRows: nil}
	s := newDraftServer(t, store)

	resp := doReq(t, s, http.MethodGet, "/api/drafts", "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "[]", strings.TrimSpace(string(body)))
}

// A draft not owned by the caller (or missing) yields no row → 404, identical
// for "not yours" and "does not exist" so ownership is never leaked.
func TestGetDraft_OtherUsersDraftIs404(t *testing.T) {
	store := &fakeDocStore{getErr: pgx.ErrNoRows}
	s := newDraftServer(t, store)

	resp := doReq(t, s, http.MethodGet, "/api/drafts/"+uuid.NewString(), "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestSaveDraft_OtherUsersDraftIs404(t *testing.T) {
	store := &fakeDocStore{saveErr: pgx.ErrNoRows}
	s := newDraftServer(t, store)

	resp := doReq(t, s, http.MethodPut, "/api/drafts/"+uuid.NewString(), `{"source_md":"x"}`)
	defer resp.Body.Close()

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestGetDraft_MalformedIDIs400(t *testing.T) {
	store := &fakeDocStore{}
	s := newDraftServer(t, store)

	resp := doReq(t, s, http.MethodGet, "/api/drafts/not-a-uuid", "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// Without a documents store the draft routes are not registered at all (404),
// while /me on the same authed group still works.
func TestDraftRoutesUnregisteredWithoutStore(t *testing.T) {
	s := newDraftServer(t, nil)

	resp := doReq(t, s, http.MethodPost, "/api/documents", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// Unauthenticated requests never reach the handler/store (middleware 401).
func TestDraftRoutesRequireAuth(t *testing.T) {
	store := &fakeDocStore{}
	s := newDraftServer(t, store)

	req, _ := http.NewRequest(http.MethodPost, "/api/documents", nil)
	resp, err := s.App().Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}
