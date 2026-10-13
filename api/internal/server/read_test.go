package server

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

// publishedRow builds a canned published-doc read row with the given access scope
// and author. rendered_html/tags/author are all populated so a 200 exercises
// the full projection.
func publishedRow(access dbgen.DocumentAccess, authorID uuid.UUID) dbgen.GetPublishedBySlugRow {
	title := "Kubernetes Guide"
	desc := "How we run k8s"
	html := "<h1>Kubernetes</h1><p>content</p>"
	name := "Ada Lovelace"
	return dbgen.GetPublishedBySlugRow{
		Slug:              "kubernetes-guide-abcd1234",
		Title:             &title,
		Description:       &desc,
		RenderedHtml:      &html,
		Access:            access,
		AuthorID:          pgUUID(authorID),
		PublishedAt:       pgtype.Timestamptz{Time: time.Now(), Valid: true},
		AuthorDisplayName: &name,
		Tags:              []string{"guide", "kubernetes"},
	}
}

// AC-8.1/8.5: a public published doc returns its cached HTML plus the display
// metadata (author, published date, tags). The handler passes the slug
// through unchanged and never exposes source_md or author_id.
func TestReadDocument_PublicReturnsRenderedHTMLAndMetadata(t *testing.T) {
	store := &fakeDocStore{getBySlugRow: publishedRow(dbgen.DocumentAccessPublic, uuid.New())}
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
	store := &fakeDocStore{getBySlugRow: publishedRow(dbgen.DocumentAccessPublic, uuid.New())}
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

// AC-8.4: a restricted doc requested by a non-author is denied with the SAME 404
// body as a missing doc — no title/existence leak, no way to distinguish the two.
func TestReadDocument_RestrictedNonAuthorIs404(t *testing.T) {
	restricted := &fakeDocStore{getBySlugRow: publishedRow(dbgen.DocumentAccessRestricted, uuid.New())}
	sr := newDraftServer(t, restricted)
	respDenied := doReq(t, sr, http.MethodGet, "/api/documents/kubernetes-guide-abcd1234", "")
	defer respDenied.Body.Close()

	missing := &fakeDocStore{getBySlugErr: pgx.ErrNoRows}
	sm := newDraftServer(t, missing)
	respMissing := doReq(t, sm, http.MethodGet, "/api/documents/kubernetes-guide-abcd1234", "")
	defer respMissing.Body.Close()

	require.Equal(t, http.StatusNotFound, respDenied.StatusCode)
	require.Equal(t, http.StatusNotFound, respMissing.StatusCode)

	// Bodies must be byte-identical so denied and missing are indistinguishable.
	var deniedBody, missingBody map[string]any
	require.NoError(t, json.NewDecoder(respDenied.Body).Decode(&deniedBody))
	require.NoError(t, json.NewDecoder(respMissing.Body).Decode(&missingBody))
	require.Equal(t, missingBody, deniedBody)
}

// An author always reads their own doc, even when it is restricted.
func TestReadDocument_RestrictedAuthorReturns200(t *testing.T) {
	store := &fakeDocStore{getBySlugRow: publishedRow(dbgen.DocumentAccessRestricted, callerUUID)}
	s := newDraftServer(t, store)

	resp := doReq(t, s, http.MethodGet, "/api/documents/kubernetes-guide-abcd1234", "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
}

// A published doc with no cached rendered_html is a data-integrity bug: surface it
// as a 500, never fabricate a render-on-read fallback.
func TestReadDocument_MissingRenderedHTMLIs500(t *testing.T) {
	row := publishedRow(dbgen.DocumentAccessPublic, uuid.New())
	row.RenderedHtml = nil
	store := &fakeDocStore{getBySlugRow: row}
	s := newDraftServer(t, store)

	resp := doReq(t, s, http.MethodGet, "/api/documents/kubernetes-guide-abcd1234", "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// CanRead is the M1 public-or-author seam M3 extends. Verify each branch directly.
func TestCanRead(t *testing.T) {
	author := pgUUID(uuid.New())
	other := pgUUID(uuid.New())

	tests := []struct {
		name     string
		caller   pgtype.UUID
		access   dbgen.DocumentAccess
		authorID pgtype.UUID
		want     bool
	}{
		{"public doc, any caller", other, dbgen.DocumentAccessPublic, author, true},
		{"restricted doc, author caller", author, dbgen.DocumentAccessRestricted, author, true},
		{"restricted doc, non-author caller", other, dbgen.DocumentAccessRestricted, author, false},
		{"public doc, author caller", author, dbgen.DocumentAccessPublic, author, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, CanRead(tc.caller, nil, tc.access, tc.authorID))
		})
	}
}
