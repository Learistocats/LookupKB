package server

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/Learistocats/LookupKB/api/internal/config"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/Learistocats/LookupKB/api/internal/search"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func publishedRowWithMarkdown(authorID uuid.UUID, md string) dbgen.GetPublishedBySlugRow {
	row := publishedRow(authorID)
	row.PublishedMd = &md
	row.UpdatedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	return row
}

// v1Resp is the status + headers of a drained response (the body is returned
// separately as a string).
type v1Resp struct {
	StatusCode int
	Header     http.Header
}

func getV1(t *testing.T, s *Server, target, accept string) (v1Resp, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Authorization", bearer(t))
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := s.App().Test(req)
	require.NoError(t, err)
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return v1Resp{StatusCode: resp.StatusCode, Header: resp.Header}, string(b)
}

func TestV1GetDocument_Formats(t *testing.T) {
	store := &fakeDocStore{getBySlugRow: publishedRowWithMarkdown(uuid.New(), "# Kubernetes\n\ncontent")}
	s := newDraftServer(t, store)
	const path = "/api/v1/documents/kubernetes-guide-abcd1234"

	// Default: the JSON envelope with both stored representations, no internals.
	resp, body := getV1(t, s, path, "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	var env map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &env))
	require.Equal(t, "kubernetes-guide-abcd1234", env["slug"])
	require.Equal(t, "Kubernetes Guide", env["title"])
	require.Equal(t, "Ada Lovelace", env["author_display_name"])
	require.Equal(t, "<h1>Kubernetes</h1><p>content</p>", env["html"])
	require.Equal(t, "# Kubernetes\n\ncontent", env["markdown"])
	require.NotEmpty(t, env["updated_at"])
	for _, k := range []string{"id", "author_id", "source_md", "status"} {
		_, has := env[k]
		require.False(t, has, "envelope must not carry %s", k)
	}

	cases := []struct {
		query, accept, ctype, want string
	}{
		{"?format=markdown", "", "text/markdown", "# Kubernetes\n\ncontent"},
		{"?format=html", "", "text/html", "<h1>Kubernetes</h1><p>content</p>"},
		{"?format=text", "", "text/plain", "Kubernetes\n\ncontent"},
		{"", "text/markdown", "text/markdown", "# Kubernetes\n\ncontent"},
		{"", "text/html", "text/html", "<h1>Kubernetes</h1><p>content</p>"},
		{"", "text/plain", "text/plain", "Kubernetes\n\ncontent"},
		// ?format wins over Accept.
		{"?format=markdown", "text/html", "text/markdown", "# Kubernetes\n\ncontent"},
	}
	for _, tc := range cases {
		resp, body := getV1(t, s, path+tc.query, tc.accept)
		require.Equal(t, http.StatusOK, resp.StatusCode, tc)
		require.Contains(t, resp.Header.Get("Content-Type"), tc.ctype, tc)
		require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
		require.Equal(t, tc.want, body, tc)
	}

	// The HTML representation is the cached column verbatim and boxed by a CSP.
	resp, _ = getV1(t, s, path+"?format=html", "")
	require.Contains(t, resp.Header.Get("Content-Security-Policy"), "sandbox")

	// Accept that matches nothing falls back to JSON; a bad ?format is a 400.
	resp, _ = getV1(t, s, path, "image/png")
	require.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	resp, _ = getV1(t, s, path+"?format=pdf", "")
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// Missing and denied slugs are the identical 404, in every format.
func TestV1GetDocument_MissingAndDeniedAreIdentical404(t *testing.T) {
	missing := &fakeDocStore{getBySlugErr: pgx.ErrNoRows}
	denied := &fakeDocStore{getBySlugRow: publishedRowWithMarkdown(uuid.New(), "secret"), deniedSlugs: []string{"kubernetes"}}

	for _, q := range []string{"", "?format=markdown", "?format=text", "?format=html"} {
		r1, b1 := getV1(t, newDraftServer(t, missing), "/api/v1/documents/x"+q, "")
		r2, b2 := getV1(t, newDraftServer(t, denied), "/api/v1/documents/x"+q, "")
		require.Equal(t, http.StatusNotFound, r1.StatusCode, b1)
		require.Equal(t, http.StatusNotFound, r2.StatusCode)
		require.Equal(t, b1, b2)
		require.NotContains(t, b2, "secret")
	}
	require.Positive(t, missing.deniedCalls, "the policy is resolved even for a missing slug (no timing oracle)")
}

// The author reads their own entry even when it carries a tag they lack.
func TestV1GetDocument_AuthorBypassesProtectedTag(t *testing.T) {
	store := &fakeDocStore{getBySlugRow: publishedRowWithMarkdown(callerUUID, "mine"), deniedSlugs: []string{"kubernetes"}}
	resp, body := getV1(t, newDraftServer(t, store), "/api/v1/documents/x?format=markdown", "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "mine", body)
}

// A published row without published_md is a data bug: 500 in every format, never
// a fallback to the working copy.
func TestV1GetDocument_MissingPublishedMarkdownIs500(t *testing.T) {
	row := publishedRow(uuid.New())
	row.PublishedMd = nil
	store := &fakeDocStore{getBySlugRow: row}
	resp, _ := getV1(t, newDraftServer(t, store), "/api/v1/documents/x?format=markdown", "")
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func listRow(slug string, at time.Time) dbgen.ListPublishedDocumentsRow {
	title := "T " + slug
	return dbgen.ListPublishedDocumentsRow{
		ID:                pgUUID(uuid.New()),
		Slug:              slug,
		Title:             &title,
		PublishedAt:       pgtype.Timestamptz{Time: at, Valid: true},
		UpdatedAt:         pgtype.Timestamptz{Time: at, Valid: true},
		AuthorDisplayName: "ada",
	}
}

func TestV1ListDocuments_PaginatesWithCursorAndPassesPolicy(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	store := &fakeDocStore{
		deniedSlugs: []string{"secret"},
		listPublishedRows: []dbgen.ListPublishedDocumentsRow{
			listRow("c", now), listRow("b", now.Add(-time.Minute)), listRow("a", now.Add(-2*time.Minute)),
		},
	}
	s := newDraftServer(t, store)

	resp, body := getV1(t, s, "/api/v1/documents?limit=2", "")
	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	var page struct {
		Documents []map[string]any `json:"documents"`
		Next      string           `json:"next_cursor"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &page))
	require.Len(t, page.Documents, 2)
	require.Equal(t, "c", page.Documents[0]["slug"])
	require.Equal(t, []any{}, page.Documents[0]["tags"], "tags is never null")
	require.NotEmpty(t, page.Next)
	for _, k := range []string{"id", "html", "markdown", "source_md"} {
		_, has := page.Documents[0][k]
		require.False(t, has, "list is metadata only: %s", k)
	}

	// The access policy reaches SQL (filtered before the LIMIT) and limit+1 rows
	// are asked for to detect the next page.
	arg := store.listPublishedArg
	require.Equal(t, pgUUID(callerUUID), arg.CallerID)
	require.Len(t, arg.DeniedIds, 1)
	require.EqualValues(t, 3, arg.MaxRows)
	require.False(t, arg.CursorPublishedAt.Valid)

	// The cursor resumes after the last row of the page.
	_, body = getV1(t, s, "/api/v1/documents?limit=2&cursor="+page.Next, "")
	arg = store.listPublishedArg
	require.True(t, arg.CursorPublishedAt.Valid)
	require.True(t, arg.CursorPublishedAt.Time.Equal(now.Add(-time.Minute)))
	require.Equal(t, store.listPublishedRows[1].ID, arg.CursorID)
	require.NotContains(t, body, "next_cursor\":\"\"")

	// Last page: no next_cursor.
	resp, body = getV1(t, s, "/api/v1/documents?limit=5", "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NotContains(t, body, "next_cursor")
}

func TestV1ListDocuments_Validation(t *testing.T) {
	store := &fakeDocStore{}
	s := newDraftServer(t, store)
	for _, q := range []string{"?limit=-1", "?limit=abc", "?cursor=!!", "?cursor=bm90LWEtY3Vyc29y"} {
		resp, _ := getV1(t, s, "/api/v1/documents"+q, "")
		require.Equal(t, http.StatusBadRequest, resp.StatusCode, q)
	}
	resp, body := getV1(t, s, "/api/v1/documents?limit=1000", "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.EqualValues(t, 101, store.listPublishedArg.MaxRows, "limit is capped at 100")
	require.JSONEq(t, `{"documents":[]}`, body)
}

// A PAT reaches the public read API (same handlers, same caller identity), and
// is still refused off the read surface.
func TestV1_ReachableWithPAT(t *testing.T) {
	tokens := newFakeTokenStore()
	docs := &fakeDocStore{
		getBySlugRow:      publishedRowWithMarkdown(uuid.New(), "# hi"),
		listPublishedRows: []dbgen.ListPublishedDocumentsRow{listRow("a", time.Now())},
	}
	cfg, err := config.Load()
	require.NoError(t, err)
	mw := auth.NewMiddleware(testTokens, tokens, slogDiscard())
	s := New(cfg, slogDiscard(), "test", WithAuth(mw), WithTokens(tokens), WithDocuments(docs),
		WithSearch(&fakeSearch{}), WithTags(&fakeTagStore{}))
	pat := createToken(t, s, `{"name":"agent"}`).Token

	for _, target := range []string{
		"/api/v1/documents", "/api/v1/documents/x?format=markdown", "/api/v1/search?q=x", "/api/v1/tags?prefix=k",
	} {
		resp := doWith(t, s, http.MethodGet, target, "Bearer "+pat, "")
		_ = resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode, target)
	}
	require.Equal(t, pgUUID(callerUUID), docs.listPublishedArg.CallerID, "the PAT acts as its user")

	resp := doWith(t, s, http.MethodGet, "/api/documents/mine", "Bearer "+pat, "")
	_ = resp.Body.Close()
	require.Equal(t, http.StatusForbidden, resp.StatusCode, "drafts stay off the PAT surface")
	resp = doWith(t, s, http.MethodGet, "/api/v1/documents", "Bearer lkb_pat_nope_nope", "")
	_ = resp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// Without a documents store the public document routes do not exist.
func TestV1_DocumentRoutesGatedOnStore(t *testing.T) {
	s := newDraftServer(t, nil)
	resp, _ := getV1(t, s, "/api/v1/documents", "")
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	resp, _ = getV1(t, s, "/api/v1/search", "")
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestV1Tags_PrefixAlias(t *testing.T) {
	store := &fakeTagStore{}
	cfg, err := config.Load()
	require.NoError(t, err)
	s := New(cfg, slogDiscard(), "test", WithAuth(auth.NewMiddleware(testTokens, nil, slogDiscard())), WithTags(store))
	resp, _ := getV1(t, s, "/api/v1/tags?prefix=kube", "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "kube", store.gotQuery)
	require.Equal(t, pgUUID(callerUUID), store.gotCaller.UserID, "caller-aware SearchTags")
}

func TestV1Search_SharesHandlerAndPassesOffset(t *testing.T) {
	next := 20
	svc := &fakeSearch{res: searchResult(next)}
	s := newSearchServer(t, svc)
	resp, body := getV1(t, s, "/api/v1/search?q=kube&tags=a,b&offset=20&limit=20", "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "kube", svc.query.Q)
	require.Equal(t, 20, svc.query.Offset)
	require.Equal(t, []string{"a", "b"}, svc.query.Filters.Tags)
	require.Equal(t, pgUUID(callerUUID), svc.caller.UserID)
	require.Contains(t, body, `"next_offset":20`)

	resp, _ = getV1(t, s, "/api/v1/search?offset=x", "")
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func searchResult(next int) search.Result {
	return search.Result{Hits: []search.Hit{}, NextOffset: &next}
}
