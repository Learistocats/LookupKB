//go:build integration

package db_test

import (
	"context"
	"testing"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/Learistocats/LookupKB/api/internal/tags"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	appdb "github.com/Learistocats/LookupKB/api/internal/db"
)

// TestTagUsageLifecycle drives the reusable SetDocumentTags/ClearDocumentTags apply
// directly through the publish / edit-republish / unpublish scenarios the publish
// gate (LOO-14/LOO-17) will invoke, asserting that document_tags membership and
// each tag's usage_count stay in sync — usage_count tracks LIVE published usage.
func TestTagUsageLifecycle(t *testing.T) {
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
	store := tags.New(pool)

	author, err := q.CreateUser(ctx, dbgen.CreateUserParams{Email: "author-sub@example.com", PasswordHash: "x"})
	require.NoError(t, err)
	doc1, err := q.CreateDraft(ctx, dbgen.CreateDraftParams{Slug: "doc-1", AuthorID: author.ID})
	require.NoError(t, err)
	doc2, err := q.CreateDraft(ctx, dbgen.CreateDraftParams{Slug: "doc-2", AuthorID: author.ID})
	require.NoError(t, err)

	usage := func(slug string) int {
		var n int
		err := pool.QueryRow(ctx, "SELECT usage_count FROM tags WHERE slug=$1", slug).Scan(&n)
		require.NoError(t, err)
		return n
	}
	docTagCount := func(id pgtype.UUID) int {
		var n int
		require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM document_tags WHERE document_id=$1", id).Scan(&n))
		return n
	}

	// --- publish doc1 with two tags (input is un-normalized) ---
	require.NoError(t, store.SetDocumentTags(ctx, doc1.ID, []string{"Go", "PostgreSQL"}))
	require.Equal(t, 2, docTagCount(doc1.ID))
	require.Equal(t, 1, usage("go"), "normalized + counted")
	require.Equal(t, 1, usage("postgresql"))

	// --- re-publish doc1 unchanged: idempotent, no count drift ---
	require.NoError(t, store.SetDocumentTags(ctx, doc1.ID, []string{"go", "postgresql"}))
	require.Equal(t, 2, docTagCount(doc1.ID))
	require.Equal(t, 1, usage("go"))
	require.Equal(t, 1, usage("postgresql"))

	// --- edit-republish doc1: swap postgresql → search ---
	require.NoError(t, store.SetDocumentTags(ctx, doc1.ID, []string{"go", "search"}))
	require.Equal(t, 2, docTagCount(doc1.ID))
	require.Equal(t, 1, usage("go"), "kept tag unchanged")
	require.Equal(t, 0, usage("postgresql"), "removed tag decremented")
	require.Equal(t, 1, usage("search"), "added tag incremented")

	// --- publish doc2 sharing the "go" tag: shared count rises to 2 ---
	require.NoError(t, store.SetDocumentTags(ctx, doc2.ID, []string{"go"}))
	require.Equal(t, 2, usage("go"), "shared across two published docs")

	// --- unpublish doc1: its tags detach, counts drop, doc2's "go" survives ---
	require.NoError(t, store.ClearDocumentTags(ctx, doc1.ID))
	require.Equal(t, 0, docTagCount(doc1.ID))
	require.Equal(t, 1, usage("go"), "only doc2 still uses it")
	require.Equal(t, 0, usage("search"))

	// --- clamp: unpublishing doc2 cannot drive a counter negative ---
	require.NoError(t, store.ClearDocumentTags(ctx, doc2.ID))
	require.Equal(t, 0, usage("go"))
	require.NoError(t, store.ClearDocumentTags(ctx, doc2.ID), "clearing an already-empty doc is a no-op")
	require.Equal(t, 0, usage("go"), "clamped at zero, never negative")
}

// TestSearchTags_AutocompleteRanking proves substring autocomplete returns matches
// ordered by usage_count (AC-6.1) and that an empty query returns popular tags.
func TestSearchTags_AutocompleteRanking(t *testing.T) {
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
	store := tags.New(pool)

	author, err := q.CreateUser(ctx, dbgen.CreateUserParams{Email: "a@example.com", PasswordHash: "x"})
	require.NoError(t, err)
	// "api" used by 2 docs, "api-gateway" by 1 — so "api" ranks first.
	d1, _ := q.CreateDraft(ctx, dbgen.CreateDraftParams{Slug: "d1", AuthorID: author.ID})
	d2, _ := q.CreateDraft(ctx, dbgen.CreateDraftParams{Slug: "d2", AuthorID: author.ID})
	d3, _ := q.CreateDraft(ctx, dbgen.CreateDraftParams{Slug: "d3", AuthorID: author.ID})
	require.NoError(t, store.SetDocumentTags(ctx, d1.ID, []string{"api"}))
	require.NoError(t, store.SetDocumentTags(ctx, d2.ID, []string{"api", "api-gateway"}))
	require.NoError(t, store.SetDocumentTags(ctx, d3.ID, []string{"unrelated"}))

	got, err := store.SearchTags(ctx, "api", 10)
	require.NoError(t, err)
	require.Len(t, got, 2, "substring match on 'api'")
	require.Equal(t, "api", got[0].Slug, "most-used first")
	require.Equal(t, int32(2), got[0].UsageCount)
	require.Equal(t, "api-gateway", got[1].Slug)

	// Partial typing normalizes ("api gate" → "api-gate") and still matches.
	got, err = store.SearchTags(ctx, "api gate", 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "api-gateway", got[0].Slug)

	// Empty query returns the most-used tags overall.
	got, err = store.SearchTags(ctx, "", 10)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(got), 3)
	require.Equal(t, "api", got[0].Slug, "popularity-ordered default")
}
