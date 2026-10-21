//go:build integration

package db_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Learistocats/LookupKB/api/internal/access"
	"github.com/Learistocats/LookupKB/api/internal/admin"
	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/Learistocats/LookupKB/api/internal/config"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/Learistocats/LookupKB/api/internal/search"
	"github.com/Learistocats/LookupKB/api/internal/server"
	"github.com/Learistocats/LookupKB/api/internal/tags"
)

const meiliKey = "integration-master-key-0123456789"

// startTestMeili boots the Meilisearch version the compose stack pins.
func startTestMeili(t *testing.T, ctx context.Context) *search.Client {
	t.Helper()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "getmeili/meilisearch:v1.12",
			ExposedPorts: []string{"7700/tcp"},
			Env: map[string]string{
				"MEILI_MASTER_KEY":   meiliKey,
				"MEILI_NO_ANALYTICS": "true",
				"MEILI_ENV":          "development",
			},
			WaitingFor: wait.ForHTTP("/health").WithPort("7700/tcp").WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Terminate(ctx) })
	host, err := c.Host(ctx)
	require.NoError(t, err)
	port, err := c.MappedPort(ctx, "7700/tcp")
	require.NoError(t, err)
	return search.NewClient(fmt.Sprintf("http://%s:%s", host, port.Port()), meiliKey)
}

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func pgID(u pgtype.UUID) string { return uuidStr(u) }

// denyOnly is a static policy source for driving search.Service directly.
type denyOnly []dbgen.ListDeniedProtectedTagsRow

func (d denyOnly) ListDeniedProtectedTags(context.Context, pgtype.UUID) ([]dbgen.ListDeniedProtectedTagsRow, error) {
	return d, nil
}

// TestAccessQueries covers ListDeniedProtectedTags and the protected-aware
// autocomplete against real Postgres.
func TestAccessQueries(t *testing.T) {
	ctx := context.Background()
	pool, actor := newFixture(t, ctx)
	q := dbgen.New(pool)
	adm := admin.New(pool)
	store := tags.New(pool)

	member, err := q.CreateUser(ctx, dbgen.CreateUserParams{Email: "member@example.com", PasswordHash: "x"})
	require.NoError(t, err)
	outsider, err := q.CreateUser(ctx, dbgen.CreateUserParams{Email: "out@example.com", PasswordHash: "x"})
	require.NoError(t, err)

	// A public tag in use, plus two protected tags granted to different groups.
	doc := newDoc(t, ctx, pool, actor, "d1")
	require.NoError(t, store.SetDocumentTags(ctx, doc, []string{"public-tag"}))
	g1, err := adm.CreateGroup(ctx, actor, "g1")
	require.NoError(t, err)
	g2, err := adm.CreateGroup(ctx, actor, "g2")
	require.NoError(t, err)
	_, err = adm.GrantTag(ctx, actor, g1.ID, "proj-a", true)
	require.NoError(t, err)
	_, err = adm.GrantTag(ctx, actor, g2.ID, "proj-b", true)
	require.NoError(t, err)
	_, err = adm.AddMember(ctx, actor, g1.ID, "member@example.com")
	require.NoError(t, err)

	slugs := func(rows []dbgen.ListDeniedProtectedTagsRow) []string {
		out := []string{}
		for _, r := range rows {
			out = append(out, r.Slug)
		}
		return out
	}
	rows, err := q.ListDeniedProtectedTags(ctx, member.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"proj-b"}, slugs(rows), "granted proj-a via g1; public tags are never denied")
	rows, err = q.ListDeniedProtectedTags(ctx, outsider.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"proj-a", "proj-b"}, slugs(rows))

	// Autocomplete: the outsider sees neither protected tag; the member sees
	// proj-a flagged protected; a super-admin sees both.
	suggest := func(c access.Caller) map[string]bool {
		got, err := store.SearchTags(ctx, "", 20, c)
		require.NoError(t, err)
		out := map[string]bool{}
		for _, r := range got {
			out[r.Slug] = r.IsProtected
		}
		return out
	}
	require.Equal(t, map[string]bool{"public-tag": false}, suggest(access.Caller{UserID: outsider.ID}))
	require.Equal(t, map[string]bool{"public-tag": false, "proj-a": true}, suggest(access.Caller{UserID: member.ID}))
	require.Equal(t, map[string]bool{"public-tag": false, "proj-a": true, "proj-b": true},
		suggest(access.Caller{UserID: outsider.ID, IsSuperAdmin: true}))
}

// TestMeiliNotInSemantics proves the access filter shape against a real
// Meilisearch: `tags NOT IN [b]` excludes a doc tagged [a, b], keeps [a] and
// untagged docs, the author branch re-admits their own doc, and labels with
// filter metacharacters are matched literally (no injection).
func TestMeiliNotInSemantics(t *testing.T) {
	ctx := context.Background()
	client := startTestMeili(t, ctx)
	require.NoError(t, client.EnsureIndex(ctx))

	me := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	other := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	quoted := `we"ird] OR tags EXISTS`
	docs := []search.IndexDoc{
		{ID: "ab", Slug: "ab", Title: "ab", Tags: []string{"a", "b"}, AuthorID: pgID(other), PublishedAt: 1},
		{ID: "a", Slug: "a", Title: "a", Tags: []string{"a"}, AuthorID: pgID(other), PublishedAt: 2},
		{ID: "none", Slug: "none", Title: "none", Tags: []string{}, AuthorID: pgID(other), PublishedAt: 3},
		{ID: "mine-b", Slug: "mine-b", Title: "mine", Tags: []string{"b"}, AuthorID: pgID(me), PublishedAt: 4},
		{ID: "weird", Slug: "weird", Title: "weird", Tags: []string{quoted}, AuthorID: pgID(other), PublishedAt: 5},
	}
	require.NoError(t, client.Upsert(ctx, docs...))
	require.Eventually(t, func() bool {
		n, err := client.DocumentCount(ctx)
		return err == nil && n == int64(len(docs))
	}, 30*time.Second, 200*time.Millisecond)

	slugsFor := func(denied []string, f search.Filters, facets ...string) ([]string, search.Result) {
		rows := make(denyOnly, 0, len(denied))
		for _, d := range denied {
			rows = append(rows, dbgen.ListDeniedProtectedTagsRow{Slug: d, Label: d})
		}
		svc := search.NewService(client, rows)
		res, err := svc.Search(ctx, search.Query{Limit: 50, Filters: f, Facets: facets}, access.Caller{UserID: me})
		require.NoError(t, err)
		out := []string{}
		for _, h := range res.Hits {
			out = append(out, h.Slug)
		}
		return out, res
	}

	got, _ := slugsFor([]string{"b"}, search.Filters{})
	require.ElementsMatch(t, []string{"a", "none", "mine-b", "weird"}, got,
		"[a,b] excluded when b is denied; untagged kept; own doc kept")

	got, _ = slugsFor([]string{quoted}, search.Filters{})
	require.ElementsMatch(t, []string{"ab", "a", "none", "mine-b"}, got,
		"a label with quotes/brackets is denied literally, not parsed as filter syntax")

	got, _ = slugsFor(nil, search.Filters{})
	require.Len(t, got, len(docs), "nothing denied → no access filter")

	// Client filters only narrow: tag=b with b denied yields just the caller's own.
	got, _ = slugsFor([]string{"b"}, search.Filters{Tags: []string{"b"}})
	require.Equal(t, []string{"mine-b"}, got)
	got, _ = slugsFor([]string{"b"}, search.Filters{AuthorID: pgID(other)})
	require.ElementsMatch(t, []string{"a", "none", "weird"}, got)
	got, _ = slugsFor(nil, search.Filters{Tags: []string{quoted}})
	require.Equal(t, []string{"weird"}, got, "a quoted client tag value is matched literally")

	// Facets are computed under the access filter: the denied tag only counts
	// the caller's own entry, and the hidden [a,b] doc does not count toward a.
	_, res := slugsFor([]string{"b"}, search.Filters{}, search.FacetTags)
	require.Equal(t, int64(1), res.Facets["tags"]["a"])
	require.Equal(t, int64(1), res.Facets["tags"]["b"])
}

// TestProtectedTagsEndToEnd is the LOO-33/LOO-39 matrix through the real HTTP
// server, Postgres, the outbox worker and Meilisearch.
func TestProtectedTagsEndToEnd(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	pool, adminID := newFixture(t, ctx) // author@example.com acts as the super-admin
	q := dbgen.New(pool)
	meili := startTestMeili(t, ctx)
	_, err := pool.Exec(ctx, "UPDATE users SET is_super_admin = true WHERE id = $1", adminID)
	require.NoError(t, err)

	user1, err := q.CreateUser(ctx, dbgen.CreateUserParams{Email: "user1@example.com", PasswordHash: "x"})
	require.NoError(t, err)
	user2, err := q.CreateUser(ctx, dbgen.CreateUserParams{Email: "user2@example.com", PasswordHash: "x"})
	require.NoError(t, err)

	tokens, err := auth.NewTokens("0123456789abcdef0123456789abcdef", time.Hour)
	require.NoError(t, err)
	cfg, err := config.Load()
	require.NoError(t, err)
	log := discardLog()
	srv := server.New(cfg, log, "test",
		server.WithAuth(auth.NewMiddleware(tokens, dbgen.New(pool), log)),
		server.WithDocuments(dbgen.New(pool)),
		server.WithTags(tags.New(pool)),
		server.WithPublish(newPublishService(pool)),
		server.WithSearch(search.NewService(meili, dbgen.New(pool))),
	)
	go search.NewWorker(dbgen.New(pool), meili, log).Run(ctx)

	bearer := func(id pgtype.UUID) string {
		raw, _, err := tokens.Issue(pgID(id), "x@example.com")
		require.NoError(t, err)
		return "Bearer " + raw
	}
	call := func(who pgtype.UUID, method, target, body string) (int, []byte) {
		var r io.Reader
		if body != "" {
			r = strings.NewReader(body)
		}
		req, _ := http.NewRequest(method, target, r)
		req.Header.Set("Authorization", bearer(who))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := srv.App().Test(req, 10_000)
		require.NoError(t, err)
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, b
	}
	// trySearchSlugs reports ok=false on a non-200 instead of failing, for polling:
	// the worker creates the Meili index asynchronously, so the first searches can
	// 503 until it exists.
	trySearchSlugs := func(who pgtype.UUID) ([]string, bool) {
		code, b := call(who, http.MethodGet, "/api/search?q=reeflect", "")
		if code != http.StatusOK {
			return nil, false
		}
		var res search.Result
		require.NoError(t, json.Unmarshal(b, &res))
		out := []string{}
		for _, h := range res.Hits {
			out = append(out, h.Slug)
		}
		return out, true
	}
	searchSlugs := func(who pgtype.UUID) []string {
		out, ok := trySearchSlugs(who)
		require.True(t, ok, "search should answer 200")
		return out
	}
	suggests := func(who pgtype.UUID) bool {
		code, b := call(who, http.MethodGet, "/api/v1/tags?q="+url.QueryEscape("reeflect"), "")
		require.Equal(t, http.StatusOK, code)
		return strings.Contains(string(b), `"slug":"reeflect"`)
	}
	publishBody := `{"title":"Reeflect roadmap","description":"Where the reeflect project is heading next.","tags":["reeflect"]}`

	// user1 publishes a reeflect entry (the tag is still public).
	doc1 := newDraftWithSource(t, ctx, pool, user1.ID, "draft-r1", "# Reeflect\n\nreeflect plans.")
	code, b := call(user1.ID, http.MethodPost, "/api/documents/"+pgID(doc1)+"/publish", publishBody)
	require.Equal(t, http.StatusOK, code, string(b))
	var pub struct {
		Slug string `json:"slug"`
	}
	require.NoError(t, json.Unmarshal(b, &pub))
	readURL := "/api/documents/" + pub.Slug

	// Before protection everyone reads/finds it.
	code, _ = call(user2.ID, http.MethodGet, readURL, "")
	require.Equal(t, http.StatusOK, code)
	require.Eventually(t, func() bool {
		slugs, ok := trySearchSlugs(user2.ID)
		return ok && len(slugs) == 1
	}, 20*time.Second, 200*time.Millisecond)

	// The admin protects reeflect by granting it to group G.
	adm := admin.New(pool)
	g, err := adm.CreateGroup(ctx, adminID, "G")
	require.NoError(t, err)
	_, err = adm.GrantTag(ctx, adminID, g.ID, "reeflect", true)
	require.NoError(t, err)

	// user2 (not in G): 404 identical to a missing slug, no search hit, no
	// autocomplete, 422 on publishing with the tag.
	code, denied := call(user2.ID, http.MethodGet, readURL, "")
	require.Equal(t, http.StatusNotFound, code)
	_, missing := call(user2.ID, http.MethodGet, "/api/documents/no-such-slug", "")
	require.JSONEq(t, string(missing), string(denied))
	require.Empty(t, searchSlugs(user2.ID), "policy change applies without a reindex")
	require.False(t, suggests(user2.ID))
	doc2 := newDraftWithSource(t, ctx, pool, user2.ID, "draft-r2", "# Mine\n\nbody")
	code, b = call(user2.ID, http.MethodPost, "/api/documents/"+pgID(doc2)+"/publish", publishBody)
	require.Equal(t, http.StatusUnprocessableEntity, code)
	require.Contains(t, string(b), `"field":"tags"`)
	require.Contains(t, string(b), `tag \"reeflect\" is protected; you don't have access to it`)
	require.Equal(t, 1, countRows(t, ctx, pool, "SELECT count(*) FROM documents WHERE id=$1 AND status='draft'", doc2),
		"refused publish leaves the draft untouched")

	// The author still reads their own entry (and finds it), but cannot re-publish
	// it while it carries a tag they lack.
	code, _ = call(user1.ID, http.MethodGet, readURL, "")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, []string{pub.Slug}, searchSlugs(user1.ID))
	code, _ = call(user1.ID, http.MethodPost, "/api/documents/"+pgID(doc1)+"/publish", publishBody)
	require.Equal(t, http.StatusUnprocessableEntity, code)

	// The super-admin always sees it, everywhere.
	code, _ = call(adminID, http.MethodGet, readURL, "")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, []string{pub.Slug}, searchSlugs(adminID))
	require.True(t, suggests(adminID))

	// Facets for user2 never count the hidden entry.
	code, b = call(user2.ID, http.MethodGet, "/api/search?facets=tags", "")
	require.Equal(t, http.StatusOK, code)
	require.NotContains(t, string(b), "reeflect")

	// Adding user2 to G restores access on the next request.
	_, err = adm.AddMember(ctx, adminID, g.ID, "user2@example.com")
	require.NoError(t, err)
	code, _ = call(user2.ID, http.MethodGet, readURL, "")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, []string{pub.Slug}, searchSlugs(user2.ID))
	require.True(t, suggests(user2.ID))
	code, b = call(user2.ID, http.MethodPost, "/api/documents/"+pgID(doc2)+"/publish", publishBody)
	require.Equal(t, http.StatusOK, code, string(b))
}
