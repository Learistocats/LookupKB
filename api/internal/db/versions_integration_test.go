//go:build integration

package db_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	appdb "github.com/Learistocats/LookupKB/api/internal/db"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/Learistocats/LookupKB/api/internal/render"
	"github.com/Learistocats/LookupKB/api/internal/tags"
	"github.com/Learistocats/LookupKB/api/internal/versions"
	"github.com/google/uuid"
)

// newVersionsFixture spins up Postgres, migrates, and returns a pool plus an author
// user id, the common preamble every version test needs.
func newVersionsFixture(t *testing.T, ctx context.Context) (*pgxpool.Pool, pgtype.UUID) {
	t.Helper()

	pgc := startTestPostgres(t, ctx)
	dsn, err := pgc.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	bootstrapRoles(t, ctx, dsn)
	require.NoError(t, appdb.RunMigrations(ctx, dsn, "up"))

	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	author, err := dbgen.New(pool).UpsertUser(ctx, dbgen.UpsertUserParams{OidcSub: "author-sub"})
	require.NoError(t, err)
	return pool, author.ID
}

func newDoc(t *testing.T, ctx context.Context, pool *pgxpool.Pool, author pgtype.UUID, slug string) pgtype.UUID {
	t.Helper()
	doc, err := dbgen.New(pool).CreateDraft(ctx, dbgen.CreateDraftParams{Slug: slug, AuthorID: author})
	require.NoError(t, err)
	return doc.ID
}

func strptr(s string) *string { return &s }

func countVersions(t *testing.T, ctx context.Context, pool *pgxpool.Pool, docID pgtype.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT count(*) FROM document_versions WHERE document_id = $1", docID).Scan(&n))
	return n
}

// TestPublishSnapshots covers the ticket's core acceptance criteria against a real
// database: first publish (AC-5.3), re-publish appends an incremented, immutable
// snapshot (AC-5.5), N publishes yield contiguous version_no 1..N, current_version_id
// tracks the latest, and the ACL/access scope is captured per-publish.
func TestPublishSnapshots(t *testing.T) {
	ctx := context.Background()
	pool, author := newVersionsFixture(t, ctx)
	q := dbgen.New(pool)
	store := versions.New(pool)
	docID := newDoc(t, ctx, pool, author, "doc-publish")

	// --- AC-5.3: first publish creates exactly one row, version_no = 1 --------
	v1, err := store.Publish(ctx, versions.Snapshot{
		DocumentID:   docID,
		Title:        strptr("Title v1"),
		Description:  strptr("Desc v1"),
		SourceMD:     strptr("# v1"),
		RenderedHTML: strptr("<h1>v1</h1>"),
		Tags:         []string{"alpha"},
		Access:       dbgen.DocumentAccessPublic,
		PublishedBy:  author,
	})
	require.NoError(t, err)
	require.Equal(t, int32(1), v1.VersionNo)
	require.Equal(t, 1, countVersions(t, ctx, pool, docID))

	// Snapshot columns match exactly what was published.
	require.Equal(t, "Title v1", *v1.Title)
	require.Equal(t, "<h1>v1</h1>", *v1.RenderedHTML)
	require.Equal(t, []string{"alpha"}, v1.Tags)
	require.Equal(t, dbgen.DocumentAccessPublic, v1.Access)

	// current_version_id points at v1.
	doc, err := q.GetDraft(ctx, dbgen.GetDraftParams{ID: docID, AuthorID: author})
	require.NoError(t, err)
	require.Equal(t, v1.ID, doc.CurrentVersionID)

	// --- AC-5.5: re-publish appends v2 with a *changed* access scope ----------
	groupA := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	v2, err := store.Publish(ctx, versions.Snapshot{
		DocumentID:   docID,
		Title:        strptr("Title v2"),
		Description:  strptr("Desc v2"),
		SourceMD:     strptr("# v2"),
		RenderedHTML: strptr("<h1>v2</h1>"),
		Tags:         []string{"alpha", "beta"},
		Access:       dbgen.DocumentAccessRestricted,
		ACLGroupIDs:  []pgtype.UUID{groupA},
		PublishedBy:  author,
	})
	require.NoError(t, err)
	require.Equal(t, int32(2), v2.VersionNo)
	require.Equal(t, 2, countVersions(t, ctx, pool, docID))

	// current_version_id now points at v2.
	doc, err = q.GetDraft(ctx, dbgen.GetDraftParams{ID: docID, AuthorID: author})
	require.NoError(t, err)
	require.Equal(t, v2.ID, doc.CurrentVersionID)

	// --- immutability + ACL correctness: v1 is untouched by the re-publish ----
	// GetVersions returns newest-first: [v2, v1].
	all, err := store.GetVersions(ctx, docID)
	require.NoError(t, err)
	require.Len(t, all, 2)
	require.Equal(t, int32(2), all[0].VersionNo)
	require.Equal(t, int32(1), all[1].VersionNo)

	// The v1 snapshot still reflects its own publish, not v2's newer values.
	gotV1 := all[1]
	require.Equal(t, "Title v1", *gotV1.Title)
	require.Equal(t, []string{"alpha"}, gotV1.Tags)
	require.Equal(t, dbgen.DocumentAccessPublic, gotV1.Access)
	require.Empty(t, gotV1.ACLGroupIDs, "v1 predates the ACL change")

	// v2 carries the new restricted scope.
	gotV2 := all[0]
	require.Equal(t, dbgen.DocumentAccessRestricted, gotV2.Access)
	require.Equal(t, []pgtype.UUID{groupA}, gotV2.ACLGroupIDs)

	// --- N publishes → contiguous version_no 1..N ----------------------------
	for i := 3; i <= 5; i++ {
		v, err := store.Publish(ctx, versions.Snapshot{
			DocumentID:  docID,
			Title:       strptr("more"),
			Access:      dbgen.DocumentAccessPublic,
			PublishedBy: author,
		})
		require.NoError(t, err)
		require.Equal(t, int32(i), v.VersionNo)
	}
	require.Equal(t, 5, countVersions(t, ctx, pool, docID))

	all, err = store.GetVersions(ctx, docID)
	require.NoError(t, err)
	require.Len(t, all, 5)
	for i, v := range all { // newest-first: 5,4,3,2,1
		require.Equal(t, int32(5-i), v.VersionNo)
	}
}

// TestVersionNoIsPerDocument proves version_no is monotonic *per document*: two
// separate documents both start at version_no = 1.
func TestVersionNoIsPerDocument(t *testing.T) {
	ctx := context.Background()
	pool, author := newVersionsFixture(t, ctx)
	store := versions.New(pool)

	docA := newDoc(t, ctx, pool, author, "doc-a")
	docB := newDoc(t, ctx, pool, author, "doc-b")

	a1, err := store.Publish(ctx, versions.Snapshot{DocumentID: docA, Access: dbgen.DocumentAccessPublic, PublishedBy: author})
	require.NoError(t, err)
	b1, err := store.Publish(ctx, versions.Snapshot{DocumentID: docB, Access: dbgen.DocumentAccessPublic, PublishedBy: author})
	require.NoError(t, err)

	require.Equal(t, int32(1), a1.VersionNo)
	require.Equal(t, int32(1), b1.VersionNo, "each document numbers its versions independently")

	a2, err := store.Publish(ctx, versions.Snapshot{DocumentID: docA, Access: dbgen.DocumentAccessPublic, PublishedBy: author})
	require.NoError(t, err)
	require.Equal(t, int32(2), a2.VersionNo, "docA advances; docB is unaffected")
	require.Equal(t, 1, countVersions(t, ctx, pool, docB))
}

// TestDraftSaveCreatesNoVersion proves the hard rule that draft saves are never
// versioned — only the publish path writes document_versions.
func TestDraftSaveCreatesNoVersion(t *testing.T) {
	ctx := context.Background()
	pool, author := newVersionsFixture(t, ctx)
	q := dbgen.New(pool)
	docID := newDoc(t, ctx, pool, author, "doc-draft-only")

	body := "# just a draft"
	_, err := q.SaveDraft(ctx, dbgen.SaveDraftParams{ID: docID, AuthorID: author, SourceMd: &body})
	require.NoError(t, err)

	require.Zero(t, countVersions(t, ctx, pool, docID), "a draft save must never create a version snapshot")
}

// TestPublishComposesRenderTagsAndStatus proves the LOO-15/16/17 primitives compose
// into one publish: render (LOO-16) → apply tags (LOO-15) → snapshot + status flip
// (LOO-17) in a single transaction, so a published document can never exist without
// its snapshot, and the snapshot captures exactly the rendered HTML and applied tags.
func TestPublishComposesRenderTagsAndStatus(t *testing.T) {
	ctx := context.Background()
	pool, author := newVersionsFixture(t, ctx)
	q := dbgen.New(pool)
	docID := newDoc(t, ctx, pool, author, "doc-compose")

	// 1. Render untrusted source to sanitized HTML (LOO-16).
	source := "# Title\n\n<script>alert(1)</script>\n\nsafe **text**"
	html, err := render.New().RenderAndSanitize(ctx, source)
	require.NoError(t, err)
	require.NotContains(t, html, "<script", "rendered HTML must be sanitized")

	// 2. Apply the tag set (LOO-15), maintaining usage_count.
	appliedTags := []string{"go", "postgres"}
	require.NoError(t, tags.New(pool).SetDocumentTags(ctx, docID, appliedTags))

	// 3. Snapshot + status transition atomically (LOO-17 primitive + the gate's job).
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := q.WithTx(tx)

	_, err = tx.Exec(ctx,
		"UPDATE documents SET status='published', published_at=now() WHERE id=$1", docID)
	require.NoError(t, err)

	v, err := versions.CreateSnapshot(ctx, qtx, versions.Snapshot{
		DocumentID:   docID,
		Title:        strptr("Title"),
		SourceMD:     &source,
		RenderedHTML: &html,
		Tags:         appliedTags,
		Access:       dbgen.DocumentAccessPublic,
		PublishedBy:  author,
	})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))

	// The published document, its status, and its snapshot are all consistent.
	doc, err := q.GetDraft(ctx, dbgen.GetDraftParams{ID: docID, AuthorID: author})
	require.NoError(t, err)
	require.Equal(t, dbgen.DocumentStatusPublished, doc.Status)
	require.True(t, doc.PublishedAt.Valid)
	require.Equal(t, v.ID, doc.CurrentVersionID)

	require.Equal(t, html, *v.RenderedHTML, "snapshot stores exactly the sanitized HTML")
	require.Equal(t, appliedTags, v.Tags)
}

// TestVersionSnapshotsImmutableToAppRole proves the DB-level immutability guarantee
// (00006): the runtime app role may append (INSERT) and read (SELECT) snapshots but
// can never UPDATE or DELETE one — "immutable" is enforced by grants, not just by the
// absence of an application code path. Mirrors assertAppRoleCannotDDL.
func TestVersionSnapshotsImmutableToAppRole(t *testing.T) {
	ctx := context.Background()

	pgc := startTestPostgres(t, ctx)
	superDSN, err := pgc.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	bootstrapRoles(t, ctx, superDSN)
	require.NoError(t, appdb.RunMigrations(ctx, superDSN, "up"))

	// Seed an author + published snapshot as the owner (superuser).
	superPool, err := pgxpool.New(ctx, superDSN)
	require.NoError(t, err)
	t.Cleanup(superPool.Close)
	author, err := dbgen.New(superPool).UpsertUser(ctx, dbgen.UpsertUserParams{OidcSub: "author-sub"})
	require.NoError(t, err)
	docID := newDoc(t, ctx, superPool, author.ID, "doc-immutable")
	v, err := versions.New(superPool).Publish(ctx, versions.Snapshot{
		DocumentID: docID, Title: strptr("v1"), Access: dbgen.DocumentAccessPublic, PublishedBy: author.ID,
	})
	require.NoError(t, err)

	// Now act as the least-privilege app role. Pass UUIDs as canonical strings —
	// the database/sql pgx driver binds those to uuid columns cleanly.
	docStr := uuid.UUID(docID.Bytes).String()
	verStr := uuid.UUID(v.ID.Bytes).String()
	authStr := uuid.UUID(author.ID.Bytes).String()

	conn := open(t, roleDSN(t, ctx, pgc, "lookupkb_app", appPassword))
	defer func() { _ = conn.Close() }()

	// SELECT is allowed.
	var n int
	require.NoError(t, conn.QueryRowContext(ctx,
		"SELECT count(*) FROM document_versions WHERE document_id = $1", docStr).Scan(&n))
	require.Equal(t, 1, n, "app role can read snapshots")

	// UPDATE and DELETE are refused by the revoked grant.
	_, err = conn.ExecContext(ctx,
		"UPDATE document_versions SET title = 'tampered' WHERE id = $1", verStr)
	require.Error(t, err, "app role must NOT be able to UPDATE a version snapshot")

	_, err = conn.ExecContext(ctx,
		"DELETE FROM document_versions WHERE id = $1", verStr)
	require.Error(t, err, "app role must NOT be able to DELETE a version snapshot")

	// INSERT (append a new snapshot) is still allowed — the table is append-only.
	_, err = conn.ExecContext(ctx,
		`INSERT INTO document_versions (document_id, version_no, access, published_by)
		 VALUES ($1, 2, 'public', $2)`, docStr, authStr)
	require.NoError(t, err, "app role can still append a new snapshot")

	// The original row is intact after the rejected mutations.
	var title string
	require.NoError(t, conn.QueryRowContext(ctx,
		"SELECT title FROM document_versions WHERE id = $1", verStr).Scan(&title))
	require.Equal(t, "v1", title)
}
