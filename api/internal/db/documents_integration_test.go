//go:build integration

package db_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	appdb "github.com/Learistocats/LookupKB/api/internal/db"
	"github.com/Learistocats/LookupKB/api/internal/render"
)

// TestDraftRoundTrip proves the walking-skeleton persistence path (LOO-11):
// CreateDraft → SaveDraft → GetDraft round-trips through Postgres, the save is
// scoped to the owner, and a different author cannot read the draft.
func TestDraftRoundTrip(t *testing.T) {
	ctx := context.Background()

	pgc := startTestPostgres(t, ctx)
	dsn, err := pgc.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	bootstrapRoles(t, ctx, dsn)
	require.NoError(t, appdb.RunMigrations(ctx, dsn, "up"))

	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	q := dbgen.New(pool)

	// documents.author_id FKs users(id): the author must exist first.
	author, err := q.UpsertUser(ctx, dbgen.UpsertUserParams{OidcSub: "author-sub"})
	require.NoError(t, err)
	other, err := q.UpsertUser(ctx, dbgen.UpsertUserParams{OidcSub: "other-sub"})
	require.NoError(t, err)

	// --- create: a bare draft with no metadata (AC-2.1) ---
	created, err := q.CreateDraft(ctx, dbgen.CreateDraftParams{
		Slug:     "draft-roundtrip",
		AuthorID: author.ID,
	})
	require.NoError(t, err)
	require.Equal(t, dbgen.DocumentStatusDraft, created.Status, "new documents start as draft")
	require.Nil(t, created.Title, "no metadata required to create")
	require.Nil(t, created.SourceMd)

	// --- save: overwrite the working copy in place (AC-2.2) ---
	title := "My Draft"
	body := "# Hello\n\nsome markdown"
	saved, err := q.SaveDraft(ctx, dbgen.SaveDraftParams{
		ID:       created.ID,
		AuthorID: author.ID,
		Title:    &title,
		SourceMd: &body,
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, saved.ID, "save overwrites in place, not a new row")
	require.Equal(t, dbgen.DocumentStatusDraft, saved.Status, "save must never flip status")

	// --- read back: exactly what was last saved (Postgres persistence) ---
	got, err := q.GetDraft(ctx, dbgen.GetDraftParams{ID: created.ID, AuthorID: author.ID})
	require.NoError(t, err)
	require.NotNil(t, got.SourceMd)
	require.Equal(t, body, *got.SourceMd)
	require.Equal(t, title, *got.Title)

	// --- ownership: a different author sees no row on read or save (AC-2.3) ---
	_, err = q.GetDraft(ctx, dbgen.GetDraftParams{ID: created.ID, AuthorID: other.ID})
	require.True(t, errors.Is(err, pgx.ErrNoRows), "another author must not read the draft")

	_, err = q.SaveDraft(ctx, dbgen.SaveDraftParams{ID: created.ID, AuthorID: other.ID, SourceMd: &body})
	require.True(t, errors.Is(err, pgx.ErrNoRows), "another author must not write the draft")

	// The draft is untouched after the rejected cross-author write.
	stillOurs, err := q.GetDraft(ctx, dbgen.GetDraftParams{ID: created.ID, AuthorID: author.ID})
	require.NoError(t, err)
	require.Equal(t, body, *stillOurs.SourceMd)

	// Exactly one documents row exists (no accidental version/duplicate rows).
	var count int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM documents").Scan(&count))
	require.Equal(t, 1, count)
	var versions int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM document_versions").Scan(&versions))
	require.Zero(t, versions, "draft saves must not create version snapshots")

	// AC-2.4 / T-2.3: a draft save must NEVER enqueue a search-index write. Indexing
	// is publish-only (05-search-and-indexing.md §4) — there is no draft-save outbox
	// path at all, so after create + save the outbox is still empty.
	var outbox int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM index_outbox").Scan(&outbox))
	require.Zero(t, outbox, "drafts must never enqueue an index_outbox row")

	// AC-2.5: the workspace list returns the author's own draft, and never another
	// author's — the `other` user's list is empty even though a draft exists.
	mine, err := q.ListDrafts(ctx, author.ID)
	require.NoError(t, err)
	require.Len(t, mine, 1)
	require.Equal(t, created.ID, mine[0].ID)

	theirs, err := q.ListDrafts(ctx, other.ID)
	require.NoError(t, err)
	require.Empty(t, theirs, "another author's workspace must not list this draft")
}

// TestSetRenderedHTMLCachesSanitized proves the LOO-16 producer guarantee end to
// end: what render.RenderAndSanitize produces is exactly what lands in
// documents.rendered_html, and the persisted, readable HTML is fully sanitized —
// no unsanitized author HTML ever touches the DB (06-security.md §2; ticket AC
// "verify rendered_html is fully sanitized before it's ever readable").
func TestSetRenderedHTMLCachesSanitized(t *testing.T) {
	ctx := context.Background()

	pgc := startTestPostgres(t, ctx)
	dsn, err := pgc.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	bootstrapRoles(t, ctx, dsn)
	require.NoError(t, appdb.RunMigrations(ctx, dsn, "up"))

	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	q := dbgen.New(pool)

	author, err := q.UpsertUser(ctx, dbgen.UpsertUserParams{OidcSub: "author-sub"})
	require.NoError(t, err)

	created, err := q.CreateDraft(ctx, dbgen.CreateDraftParams{Slug: "draft-render", AuthorID: author.ID})
	require.NoError(t, err)
	require.Nil(t, created.RenderedHtml, "a fresh draft has no cached HTML")

	// A malicious source with both a working GFM feature and XSS payloads.
	source := "# Notes\n\n" +
		"<script>alert(1)</script>\n\n" +
		"[x](javascript:alert(1)) and <img src=x onerror=alert(1)>\n\n" +
		"| a | b |\n|--|--|\n| 1 | 2 |\n"

	html, err := render.New().RenderAndSanitize(ctx, source)
	require.NoError(t, err)

	// Persist via the sole write path to rendered_html.
	require.NoError(t, q.SetRenderedHTML(ctx, dbgen.SetRenderedHTMLParams{
		ID:           created.ID,
		RenderedHtml: &html,
	}))

	// Read the stored value back and assert it is the sanitized output and safe.
	got, err := q.GetDraft(ctx, dbgen.GetDraftParams{ID: created.ID, AuthorID: author.ID})
	require.NoError(t, err)
	require.NotNil(t, got.RenderedHtml)
	require.Equal(t, html, *got.RenderedHtml, "stored HTML must be exactly RenderAndSanitize output")

	stored := strings.ToLower(*got.RenderedHtml)
	for _, tok := range []string{"<script", "javascript:", "onerror="} {
		require.NotContains(t, stored, tok, "persisted rendered_html must be sanitized")
	}
	require.Contains(t, stored, "<table>", "safe GFM structure survives into the DB")
}
