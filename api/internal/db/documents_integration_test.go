//go:build integration

package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	appdb "github.com/Learistocats/LookupKB/api/internal/db"
)

// TestDraftRoundTrip proves the walking-skeleton persistence path:
// CreateDraft → SaveDraft → GetOwnedDocument round-trips through Postgres, the save is
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
	author, err := q.CreateUser(ctx, dbgen.CreateUserParams{Email: "author-sub@example.com", PasswordHash: "x"})
	require.NoError(t, err)
	other, err := q.CreateUser(ctx, dbgen.CreateUserParams{Email: "other-sub@example.com", PasswordHash: "x"})
	require.NoError(t, err)

	// --- create: a bare draft with no metadata ---
	created, err := q.CreateDraft(ctx, dbgen.CreateDraftParams{
		Slug:     "draft-roundtrip",
		AuthorID: author.ID,
	})
	require.NoError(t, err)
	require.Equal(t, dbgen.DocumentStatusDraft, created.Status, "new documents start as draft")
	require.Nil(t, created.Title, "no metadata required to create")
	require.Nil(t, created.SourceMd)

	// --- save: overwrite the working copy in place ---
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
	got, err := q.GetOwnedDocument(ctx, dbgen.GetOwnedDocumentParams{ID: created.ID, AuthorID: author.ID})
	require.NoError(t, err)
	require.NotNil(t, got.SourceMd)
	require.Equal(t, body, *got.SourceMd)
	require.Equal(t, title, *got.Title)

	// --- ownership: a different author sees no row on read or save ---
	_, err = q.GetOwnedDocument(ctx, dbgen.GetOwnedDocumentParams{ID: created.ID, AuthorID: other.ID})
	require.True(t, errors.Is(err, pgx.ErrNoRows), "another author must not read the draft")

	_, err = q.SaveDraft(ctx, dbgen.SaveDraftParams{ID: created.ID, AuthorID: other.ID, SourceMd: &body})
	require.True(t, errors.Is(err, pgx.ErrNoRows), "another author must not write the draft")

	// The draft is untouched after the rejected cross-author write.
	stillOurs, err := q.GetOwnedDocument(ctx, dbgen.GetOwnedDocumentParams{ID: created.ID, AuthorID: author.ID})
	require.NoError(t, err)
	require.Equal(t, body, *stillOurs.SourceMd)

	// Exactly one documents row exists (no accidental duplicate rows).
	var count int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM documents").Scan(&count))
	require.Equal(t, 1, count)

	// a draft save must NEVER enqueue a search-index write. Indexing
	// is publish-only (05-search-and-indexing.md §4) — there is no draft-save outbox
	// path at all, so after create + save the outbox is still empty.
	var outbox int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM index_outbox").Scan(&outbox))
	require.Zero(t, outbox, "drafts must never enqueue an index_outbox row")

	// the workspace list returns the author's own draft, and never another
	// author's — the `other` user's list is empty even though a draft exists.
	mine, err := q.ListDrafts(ctx, author.ID)
	require.NoError(t, err)
	require.Len(t, mine, 1)
	require.Equal(t, created.ID, mine[0].ID)

	theirs, err := q.ListDrafts(ctx, other.ID)
	require.NoError(t, err)
	require.Empty(t, theirs, "another author's workspace must not list this draft")
}
