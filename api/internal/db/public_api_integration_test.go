//go:build integration

package db_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/Learistocats/LookupKB/api/internal/admin"
	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/Learistocats/LookupKB/api/internal/config"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/Learistocats/LookupKB/api/internal/search"
	"github.com/Learistocats/LookupKB/api/internal/server"
	"github.com/Learistocats/LookupKB/api/internal/tags"
)

// apiHarness is the full HTTP server over real Postgres + Meilisearch + the outbox
// worker, with session JWTs and PATs both enabled — the shape cmd/api wires.
type apiHarness struct {
	t      *testing.T
	ctx    context.Context
	pool   *pgxpool.Pool
	srv    *server.Server
	tokens *auth.Tokens
	admin  pgtype.UUID // author@example.com, promoted to super-admin
}

func newAPIHarness(t *testing.T, extra ...server.Option) *apiHarness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	pool, adminID := newFixture(t, ctx)
	_, err := pool.Exec(ctx, "UPDATE users SET is_super_admin = true WHERE id = $1", adminID)
	require.NoError(t, err)
	meili := startTestMeili(t, ctx)

	tokens, err := auth.NewTokens("0123456789abcdef0123456789abcdef", time.Hour)
	require.NoError(t, err)
	cfg, err := config.Load()
	require.NoError(t, err)
	log := discardLog()
	q := dbgen.New(pool)
	mw := auth.NewMiddleware(tokens, q, log)
	opts := append([]server.Option{
		server.WithAuth(mw),
		server.WithTokens(q),
		server.WithDocuments(q),
		server.WithTags(tags.New(pool)),
		server.WithPublish(newPublishService(pool)),
		server.WithSearch(search.NewService(meili, q)),
	}, extra...)
	srv := server.New(cfg, log, "test", opts...)
	go search.NewWorker(q, meili, log).Run(ctx)
	return &apiHarness{t: t, ctx: ctx, pool: pool, srv: srv, tokens: tokens, admin: adminID}
}

func (h *apiHarness) user(email string) pgtype.UUID {
	h.t.Helper()
	u, err := dbgen.New(h.pool).CreateUser(h.ctx, dbgen.CreateUserParams{Email: email, PasswordHash: "x"})
	require.NoError(h.t, err)
	return u.ID
}

// session is the BFF's credential for the user.
func (h *apiHarness) session(id pgtype.UUID) string {
	h.t.Helper()
	raw, _, err := h.tokens.Issue(pgID(id), "x@example.com")
	require.NoError(h.t, err)
	return "Bearer " + raw
}

// pat mints a PAT through the real management route and returns (header, id).
func (h *apiHarness) pat(id pgtype.UUID) (string, string) {
	h.t.Helper()
	code, b := h.call(h.session(id), http.MethodPost, "/api/tokens", `{"name":"agent"}`, nil)
	require.Equal(h.t, http.StatusCreated, code, string(b))
	var tok struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	require.NoError(h.t, json.Unmarshal(b, &tok))
	return "Bearer " + tok.Token, tok.ID
}

func (h *apiHarness) call(authz, method, target, body string, headers map[string]string) (int, []byte) {
	h.t.Helper()
	code, b, _ := h.callH(authz, method, target, body, headers)
	return code, b
}

func (h *apiHarness) callH(authz, method, target, body string, headers map[string]string) (int, []byte, http.Header) {
	h.t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, target, r)
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := h.srv.App().Test(req, 10_000)
	require.NoError(h.t, err)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b, resp.Header
}

// publish creates a draft with source through the real routes and publishes it;
// returns (doc id, published slug).
func (h *apiHarness) publish(author pgtype.UUID, title, source string, tagSlugs ...string) (string, string) {
	h.t.Helper()
	s := h.session(author)
	code, b := h.call(s, http.MethodPost, "/api/documents", "", nil)
	require.Equal(h.t, http.StatusCreated, code, string(b))
	var d struct {
		ID string `json:"id"`
	}
	require.NoError(h.t, json.Unmarshal(b, &d))
	h.save(author, d.ID, source)
	if len(tagSlugs) == 0 {
		tagSlugs = []string{"general"} // publish requires at least one tag
	}
	tagsJSON, _ := json.Marshal(tagSlugs)
	body := `{"title":"` + title + `","description":"A description long enough to pass.","tags":` + string(tagsJSON) + `}`
	code, b = h.call(s, http.MethodPost, "/api/documents/"+d.ID+"/publish", body, nil)
	require.Equal(h.t, http.StatusOK, code, string(b))
	var pub struct {
		Slug string `json:"slug"`
	}
	require.NoError(h.t, json.Unmarshal(b, &pub))
	return d.ID, pub.Slug
}

func (h *apiHarness) save(author pgtype.UUID, docID, source string) {
	h.t.Helper()
	src, _ := json.Marshal(source)
	code, b := h.call(h.session(author), http.MethodPut, "/api/drafts/"+docID, `{"source_md":`+string(src)+`}`, nil)
	require.Equal(h.t, http.StatusOK, code, string(b))
}

// protect grants tag to a new group (making it protected) with the given members.
func (h *apiHarness) protect(tag string, memberEmails ...string) {
	h.t.Helper()
	adm := admin.New(h.pool)
	g, err := adm.CreateGroup(h.ctx, h.admin, "group-"+tag)
	require.NoError(h.t, err)
	_, err = adm.GrantTag(h.ctx, h.admin, g.ID, tag, true)
	require.NoError(h.t, err)
	for _, e := range memberEmails {
		_, err = adm.AddMember(h.ctx, h.admin, g.ID, e)
		require.NoError(h.t, err)
	}
}

func listSlugs(t *testing.T, h *apiHarness, authz, query string) ([]string, string) {
	t.Helper()
	code, b := h.call(authz, http.MethodGet, "/api/v1/documents"+query, "", nil)
	require.Equal(t, http.StatusOK, code, string(b))
	var page struct {
		Documents []struct {
			Slug string `json:"slug"`
		} `json:"documents"`
		Next string `json:"next_cursor"`
	}
	require.NoError(t, json.Unmarshal(b, &page))
	out := []string{}
	for _, d := range page.Documents {
		out = append(out, d.Slug)
	}
	return out, page.Next
}

// tryV1SearchSlugs reports ok=false on a non-200 instead of failing, for polling:
// the worker creates the Meili index asynchronously, so early searches can 503.
func tryV1SearchSlugs(t *testing.T, h *apiHarness, authz, q string) ([]string, bool) {
	t.Helper()
	code, b := h.call(authz, http.MethodGet, "/api/v1/search?q="+q, "", nil)
	if code != http.StatusOK {
		return nil, false
	}
	var res search.Result
	require.NoError(t, json.Unmarshal(b, &res))
	out := []string{}
	for _, hit := range res.Hits {
		out = append(out, hit.Slug)
	}
	return out, true
}

func v1SearchSlugs(t *testing.T, h *apiHarness, authz, q string) []string {
	t.Helper()
	out, ok := tryV1SearchSlugs(t, h, authz, q)
	require.True(t, ok, "search should answer 200")
	return out
}

// TestPublicReadAPI is the matrix: PAT and session both work; protected
// tags hide an entry from a PAT by slug (identical 404), list and search; the
// published Markdown is the published source, not a later draft edit; drafts
// never appear; a revoked PAT is 401.
func TestPublicReadAPI(t *testing.T) {
	h := newAPIHarness(t)
	alice := h.user("alice@example.com") // member of the protected group
	bob := h.user("bob@example.com")     // not a member

	secretID, secretSlug := h.publish(alice, "Zircon secret plan", "# Zircon\n\nzircon secret body", "zircon-team")
	_, openSlug := h.publish(alice, "Zircon public notes", "# Notes\n\nzircon public body")
	draftResp, draftBody := h.call(h.session(alice), http.MethodPost, "/api/documents", "", nil)
	require.Equal(t, http.StatusCreated, draftResp, string(draftBody))
	var draft struct {
		ID   string `json:"id"`
		Slug string `json:"slug"`
	}
	require.NoError(t, json.Unmarshal(draftBody, &draft))
	h.save(alice, draft.ID, "zircon draft only")
	h.protect("zircon-team", "alice@example.com")

	bobPAT, bobPATID := h.pat(bob)
	alicePAT, _ := h.pat(alice)

	// Bob's PAT: the protected entry is an identical 404, and absent from list
	// and search. His session sees exactly the same.
	for _, cred := range []string{bobPAT, h.session(bob)} {
		code, denied := h.call(cred, http.MethodGet, "/api/v1/documents/"+secretSlug, "", nil)
		require.Equal(t, http.StatusNotFound, code)
		_, missing := h.call(cred, http.MethodGet, "/api/v1/documents/no-such-slug", "", nil)
		require.JSONEq(t, string(missing), string(denied))
		for _, f := range []string{"markdown", "text", "html"} {
			code, b := h.call(cred, http.MethodGet, "/api/v1/documents/"+secretSlug+"?format="+f, "", nil)
			require.Equal(t, http.StatusNotFound, code)
			require.NotContains(t, string(b), "zircon")
		}
		got, _ := listSlugs(t, h, cred, "")
		require.Equal(t, []string{openSlug}, got)
	}
	require.Eventually(t, func() bool {
		slugs, ok := tryV1SearchSlugs(t, h, alicePAT, "zircon")
		return ok && len(slugs) == 2
	}, 20*time.Second, 200*time.Millisecond, "both published entries indexed")
	require.Equal(t, []string{openSlug}, v1SearchSlugs(t, h, bobPAT, "zircon"))

	// Alice's PAT reads both; drafts never appear anywhere.
	got, _ := listSlugs(t, h, alicePAT, "")
	require.ElementsMatch(t, []string{secretSlug, openSlug}, got)
	code, _ := h.call(alicePAT, http.MethodGet, "/api/v1/documents/"+draft.Slug, "", nil)
	require.Equal(t, http.StatusNotFound, code, "a draft slug is not readable, even by its author")
	require.NotContains(t, v1SearchSlugs(t, h, alicePAT, "draft"), draft.Slug)

	// format=markdown is the PUBLISHED source: a later working-copy save is not
	// served until a re-publish.
	h.save(alice, secretID, "# Zircon\n\nUNPUBLISHED EDIT")
	code, md, hdr := h.callH(alicePAT, http.MethodGet, "/api/v1/documents/"+secretSlug+"?format=markdown", "", nil)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "# Zircon\n\nzircon secret body", string(md))
	require.Contains(t, hdr.Get("Content-Type"), "text/markdown")
	_, txt, hdr := h.callH(alicePAT, http.MethodGet, "/api/v1/documents/"+secretSlug, "", map[string]string{"Accept": "text/plain"})
	require.Equal(t, "Zircon\n\nzircon secret body", string(txt))
	require.Contains(t, hdr.Get("Content-Type"), "text/plain")
	code, envelope := h.call(h.session(alice), http.MethodGet, "/api/v1/documents/"+secretSlug, "", nil)
	require.Equal(t, http.StatusOK, code)
	require.NotContains(t, string(envelope), "UNPUBLISHED")

	code, b := h.call(h.session(alice), http.MethodPost, "/api/documents/"+secretID+"/publish",
		`{"title":"Zircon secret plan","description":"A description long enough to pass.","tags":["zircon-team"]}`, nil)
	require.Equal(t, http.StatusOK, code, string(b))
	_, md = h.call(alicePAT, http.MethodGet, "/api/v1/documents/"+secretSlug+"?format=markdown", "", nil)
	require.Equal(t, "# Zircon\n\nUNPUBLISHED EDIT", string(md), "re-publish refreshes published_md")

	// Unpublish clears published_md and drops the entry from the catalogue.
	code, b = h.call(h.session(alice), http.MethodPost, "/api/documents/"+secretID+"/unpublish", "", nil)
	require.Equal(t, http.StatusOK, code, string(b))
	require.Equal(t, 1, countRows(t, h.ctx, h.pool,
		"SELECT count(*) FROM documents WHERE id = $1 AND published_md IS NULL", secretID))
	got, _ = listSlugs(t, h, alicePAT, "")
	require.Equal(t, []string{openSlug}, got)

	// Cursor pagination walks every readable entry exactly once, newest first.
	var more []string
	for i := 0; i < 3; i++ {
		_, s := h.publish(bob, "Page entry", "page body")
		more = append([]string{s}, more...)
	}
	var walked []string
	cursor := ""
	for {
		page, next := listSlugs(t, h, bobPAT, "?limit=2&cursor="+cursor)
		require.LessOrEqual(t, len(page), 2)
		walked = append(walked, page...)
		if next == "" {
			break
		}
		cursor = next
	}
	require.Equal(t, append(more, openSlug), walked)

	// PAT stays on the read surface; revoked ⇒ 401.
	code, _ = h.call(bobPAT, http.MethodGet, "/api/drafts", "", nil)
	require.Equal(t, http.StatusForbidden, code)
	code, _ = h.call(h.session(bob), http.MethodDelete, "/api/tokens/"+bobPATID, "", nil)
	require.Equal(t, http.StatusNoContent, code)
	code, _ = h.call(bobPAT, http.MethodGet, "/api/v1/documents", "", nil)
	require.Equal(t, http.StatusUnauthorized, code)
}
