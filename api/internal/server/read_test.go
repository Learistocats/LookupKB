package server

import (
	"encoding/json"
	"net/http"
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

// publishedRow builds a canned published-doc read row by the given author, tagged
// guide + kubernetes. rendered_html/tags/author are all populated so a 200
// exercises the full projection.
func publishedRow(authorID uuid.UUID) dbgen.GetPublishedBySlugRow {
	title := "Kubernetes Guide"
	desc := "How we run k8s"
	html := "<h1>Kubernetes</h1><p>content</p>"
	name := "Ada Lovelace"
	return dbgen.GetPublishedBySlugRow{
		Slug:              "kubernetes-guide-abcd1234",
		Title:             &title,
		Description:       &desc,
		RenderedHtml:      &html,
		AuthorID:          pgUUID(authorID),
		PublishedAt:       pgtype.Timestamptz{Time: time.Now(), Valid: true},
		AuthorDisplayName: name,
		Tags:              []string{"guide", "kubernetes"},
		TagSlugs:          []string{"guide", "kubernetes"},
	}
}

// AC-8.1/8.5: a public published doc returns its cached HTML plus the display
// metadata (author, published date, tags). The handler passes the slug
// through unchanged and never exposes source_md or author_id.
func TestReadDocument_PublicReturnsRenderedHTMLAndMetadata(t *testing.T) {
	store := &fakeDocStore{getBySlugRow: publishedRow(uuid.New())}
	s := newDraftServer(t, store)

	resp := doReq(t, s, http.MethodGet, "/api/documents/kubernetes-guide-abcd1234", "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "kubernetes-guide-abcd1234", store.getBySlugArg)

	var got readResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Equal(t, "<h1>Kubernetes</h1><p>content</p>", got.RenderedHTML)
	require.NotNil(t, got.AuthorDisplayName)
	require.Equal(t, "Ada Lovelace", *got.AuthorDisplayName)
	require.Equal(t, []string{"guide", "kubernetes"}, got.Tags)
	require.NotEmpty(t, got.PublishedAt)
}

// The published-safe projection must NOT leak source_md or author_id, even though
// the underlying query selects author_id for the access check.
func TestReadDocument_DoesNotLeakSourceOrAuthorID(t *testing.T) {
	store := &fakeDocStore{getBySlugRow: publishedRow(uuid.New())}
	s := newDraftServer(t, store)

	resp := doReq(t, s, http.MethodGet, "/api/documents/kubernetes-guide-abcd1234", "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var raw map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&raw))
	_, hasSource := raw["source_md"]
	_, hasAuthorID := raw["author_id"]
	require.False(t, hasSource, "source_md must not be exposed on the read path")
	require.False(t, hasAuthorID, "author_id must not be exposed on the read path")
	_, hasID := raw["id"]
	require.False(t, hasID, "a non-author never learns the internal id")
}

// The author gets the document id so the reader can link to the editor.
func TestReadDocument_AuthorGetsID(t *testing.T) {
	row := publishedRow(callerUUID)
	docID := uuid.New()
	row.ID = pgUUID(docID)
	s := newDraftServer(t, &fakeDocStore{getBySlugRow: row})

	resp := doReq(t, s, http.MethodGet, "/api/documents/kubernetes-guide-abcd1234", "")
	defer resp.Body.Close()
	var got readResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Equal(t, docID.String(), got.ID)
}

// A draft/unpublished/soft-deleted/missing slug all surface as pgx.ErrNoRows from
// the status-guarded query → 404, never partial content.
func TestReadDocument_UnpublishedSlugIs404(t *testing.T) {
	store := &fakeDocStore{getBySlugErr: pgx.ErrNoRows}
	s := newDraftServer(t, store)

	resp := doReq(t, s, http.MethodGet, "/api/documents/some-slug", "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// AC-8.4 / LOO-33: an entry carrying a protected tag the caller is not granted is
// denied with the SAME 404 body as a missing doc — no title/existence leak.
func TestReadDocument_ProtectedTagNotGrantedIs404(t *testing.T) {
	denied := &fakeDocStore{getBySlugRow: publishedRow(uuid.New()), deniedSlugs: []string{"kubernetes"}}
	sr := newDraftServer(t, denied)
	respDenied := doReq(t, sr, http.MethodGet, "/api/documents/kubernetes-guide-abcd1234", "")
	defer respDenied.Body.Close()

	missing := &fakeDocStore{getBySlugErr: pgx.ErrNoRows}
	sm := newDraftServer(t, missing)
	respMissing := doReq(t, sm, http.MethodGet, "/api/documents/kubernetes-guide-abcd1234", "")
	defer respMissing.Body.Close()

	require.Equal(t, http.StatusNotFound, respDenied.StatusCode)
	require.Equal(t, http.StatusNotFound, respMissing.StatusCode)
	require.Equal(t, 1, missing.deniedCalls, "the policy is loaded even for a missing slug (same work)")

	// Bodies must be byte-identical so denied and missing are indistinguishable.
	var deniedBody, missingBody map[string]any
	require.NoError(t, json.NewDecoder(respDenied.Body).Decode(&deniedBody))
	require.NoError(t, json.NewDecoder(respMissing.Body).Decode(&missingBody))
	require.Equal(t, missingBody, deniedBody)
}

// A group member granted the protected tag (it is not in their denied set) reads.
func TestReadDocument_GrantedMemberReturns200(t *testing.T) {
	store := &fakeDocStore{getBySlugRow: publishedRow(uuid.New()), deniedSlugs: []string{"hr"}}
	resp := doReq(t, newDraftServer(t, store), http.MethodGet, "/api/documents/kubernetes-guide-abcd1234", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

// The ALL rule: granted one protected tag but denied another on the same entry → 404.
func TestReadDocument_AllProtectedTagsRequired(t *testing.T) {
	row := publishedRow(uuid.New())
	row.TagSlugs = []string{"hr", "reeflect"}
	store := &fakeDocStore{getBySlugRow: row, deniedSlugs: []string{"reeflect"}}
	resp := doReq(t, newDraftServer(t, store), http.MethodGet, "/api/documents/kubernetes-guide-abcd1234", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// An author always reads their own entry, even one carrying a tag they lack.
func TestReadDocument_AuthorWithDeniedTagReturns200(t *testing.T) {
	store := &fakeDocStore{getBySlugRow: publishedRow(callerUUID), deniedSlugs: []string{"kubernetes"}}
	resp := doReq(t, newDraftServer(t, store), http.MethodGet, "/api/documents/kubernetes-guide-abcd1234", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

// A super-admin reads every published entry; the policy query is skipped.
func TestReadDocument_SuperAdminReturns200(t *testing.T) {
	store := &fakeDocStore{getBySlugRow: publishedRow(uuid.New()), deniedSlugs: []string{"kubernetes"}}
	cfg, err := config.Load()
	require.NoError(t, err)
	s := New(cfg, slogDiscard(), "test",
		WithAuth(auth.NewMiddleware(testTokens, &fakeDirectory{super: true}, slogDiscard())),
		WithDocuments(store))
	resp := doReq(t, s, http.MethodGet, "/api/documents/kubernetes-guide-abcd1234", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Zero(t, store.deniedCalls)
}

// A published doc with no cached rendered_html is a data-integrity bug: surface it
// as a 500, never fabricate a render-on-read fallback.
func TestReadDocument_MissingRenderedHTMLIs500(t *testing.T) {
	row := publishedRow(uuid.New())
	row.RenderedHtml = nil
	store := &fakeDocStore{getBySlugRow: row}
	s := newDraftServer(t, store)

	resp := doReq(t, s, http.MethodGet, "/api/documents/kubernetes-guide-abcd1234", "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}
