package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/Learistocats/LookupKB/api/internal/config"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/Learistocats/LookupKB/api/internal/publish"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

// fakePublishService records its inputs and returns canned outcomes so the handler
// (status-code mapping, per-field 422 shape, 404 body) is tested without the real
// orchestrator, database or renderer.
type fakePublishService struct {
	publishIn   publish.Input
	publishRes  *publish.Result
	publishErrs []publish.FieldError
	publishErr  error

	unpubDocID    pgtype.UUID
	unpubAuthorID pgtype.UUID
	unpubRes      *publish.Result
	unpubErr      error
}

func (f *fakePublishService) Publish(_ context.Context, in publish.Input) (*publish.Result, []publish.FieldError, error) {
	f.publishIn = in
	return f.publishRes, f.publishErrs, f.publishErr
}

func (f *fakePublishService) Unpublish(_ context.Context, docID, authorID pgtype.UUID) (*publish.Result, error) {
	f.unpubDocID = docID
	f.unpubAuthorID = authorID
	return f.unpubRes, f.unpubErr
}

// publishedDoc builds a canned published row owned by callerUUID.
func publishedDoc(id uuid.UUID) dbgen.Document {
	title := "How to deploy"
	desc := "A short but sufficient description of the thing."
	return dbgen.Document{
		ID:          pgUUID(id),
		Slug:        "how-to-deploy-abcd1234",
		AuthorID:    pgUUID(callerUUID),
		Status:      dbgen.DocumentStatusPublished,
		Title:       &title,
		Description: &desc,
		Access:      dbgen.DocumentAccessPublic,
		PublishedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		UpdatedAt:   pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
}

// newPublishServer wires a Server with the real auth middleware and the given
// publish service, so requests exercise the full authenticated route path.
func newPublishServer(t *testing.T, svc PublishService) *Server {
	t.Helper()
	cfg, err := config.Load()
	require.NoError(t, err)

	mw := auth.NewMiddleware(testTokens, nil, slogDiscard())

	return New(cfg, slogDiscard(), "test", WithAuth(mw), WithPublish(svc))
}

// AC-5.3: valid metadata on confirm → 200 published, with the caller's inputs
// forwarded to the service (docID from the path, author from the verified token).
func TestPublish_ValidReturnsPublished(t *testing.T) {
	id := uuid.New()
	svc := &fakePublishService{publishRes: &publish.Result{Document: publishedDoc(id)}}
	s := newPublishServer(t, svc)

	body := `{"title":"How to deploy","description":"A short but sufficient description.","tags":["ops","deploy"]}`
	resp := doReq(t, s, http.MethodPost, "/api/documents/"+id.String()+"/publish", body)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, pgUUID(id), svc.publishIn.DocID)
	require.Equal(t, pgUUID(callerUUID), svc.publishIn.AuthorID)
	require.Equal(t, []string{"ops", "deploy"}, svc.publishIn.Tags)

	var got publishedResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Equal(t, "published", got.Status)
	require.Equal(t, "public", got.Access)
}

// AC-5.2 / T-5.2: a metadata violation → 422 with per-field errors (not a blob),
// and the document is unchanged (the handler returns the errors, no 5xx).
func TestPublish_InvalidReturns422PerField(t *testing.T) {
	id := uuid.New()
	svc := &fakePublishService{publishErrs: []publish.FieldError{
		{Field: publish.FieldTitle, Message: "title must be 3–120 characters (got 1)"},
		{Field: publish.FieldTags, Message: "at least 1 tag(s) required"},
	}}
	s := newPublishServer(t, svc)

	resp := doReq(t, s, http.MethodPost, "/api/documents/"+id.String()+"/publish", `{"title":"x"}`)
	defer resp.Body.Close()

	require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)

	var got struct {
		Errors []publish.FieldError `json:"errors"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Len(t, got.Errors, 2)
	require.Equal(t, publish.FieldTitle, got.Errors[0].Field)
	require.Equal(t, publish.FieldTags, got.Errors[1].Field)
}

// A missing/cross-author document → 404 with the shared not-found body (no leak).
func TestPublish_NotFoundReturns404(t *testing.T) {
	id := uuid.New()
	svc := &fakePublishService{publishErr: publish.ErrNotFound}
	s := newPublishServer(t, svc)

	resp := doReq(t, s, http.MethodPost, "/api/documents/"+id.String()+"/publish", `{"title":"whatever"}`)
	defer resp.Body.Close()

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// An unparseable document id → 400 before the service is ever called.
func TestPublish_BadIDReturns400(t *testing.T) {
	svc := &fakePublishService{}
	s := newPublishServer(t, svc)

	resp := doReq(t, s, http.MethodPost, "/api/documents/not-a-uuid/publish", `{"title":"whatever"}`)
	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// AC-5.6: unpublish → 200 with the caller's inputs forwarded to the service.
func TestUnpublish_Returns200(t *testing.T) {
	id := uuid.New()
	doc := publishedDoc(id)
	doc.Status = dbgen.DocumentStatusUnpublished
	svc := &fakePublishService{unpubRes: &publish.Result{Document: doc}}
	s := newPublishServer(t, svc)

	resp := doReq(t, s, http.MethodPost, "/api/documents/"+id.String()+"/unpublish", "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, pgUUID(id), svc.unpubDocID)
	require.Equal(t, pgUUID(callerUUID), svc.unpubAuthorID)

	var got publishedResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Equal(t, "unpublished", got.Status)
}

func TestUnpublish_NotFoundReturns404(t *testing.T) {
	id := uuid.New()
	svc := &fakePublishService{unpubErr: publish.ErrNotFound}
	s := newPublishServer(t, svc)

	resp := doReq(t, s, http.MethodPost, "/api/documents/"+id.String()+"/unpublish", "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}
