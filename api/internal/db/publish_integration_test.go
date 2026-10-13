//go:build integration

package db_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/Learistocats/LookupKB/api/internal/publish"
	"github.com/Learistocats/LookupKB/api/internal/render"
)

// uuidStr renders a pgtype.UUID as the canonical string form audit_log.target_id
// stores (the publish service writes uuid.String()).
func uuidStr(u pgtype.UUID) string { return uuid.UUID(u.Bytes).String() }

// newPublishService builds a real publish.Service over the fixture pool. A discard
// logger keeps test output clean; the renderer uses production defaults.
func newPublishService(pool *pgxpool.Pool) *publish.Service {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return publish.NewService(pool, render.New(), log)
}

func newDraftWithSource(t *testing.T, ctx context.Context, pool *pgxpool.Pool, author pgtype.UUID, slug, source string) pgtype.UUID {
	t.Helper()
	docID := newDoc(t, ctx, pool, author, slug)
	_, err := dbgen.New(pool).SaveDraft(ctx, dbgen.SaveDraftParams{ID: docID, AuthorID: author, SourceMd: &source})
	require.NoError(t, err)
	return docID
}

func countRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(ctx, sql, args...).Scan(&n))
	return n
}

func tagUsage(t *testing.T, ctx context.Context, pool *pgxpool.Pool, slug string) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(ctx, "SELECT usage_count FROM tags WHERE slug = $1", slug).Scan(&n))
	return n
}

// TestPublishFlow_FirstPublish covers AC-5.3 end-to-end through the real service:
// valid metadata → published, published_at set, a title-derived slug,
// live tag membership + usage_count, an audit row, and an enqueued index upsert.
func TestPublishFlow_FirstPublish(t *testing.T) {
	ctx := context.Background()
	pool, author := newFixture(t, ctx)
	svc := newPublishService(pool)
	q := dbgen.New(pool)

	docID := newDraftWithSource(t, ctx, pool, author, "draft-first", "# Hello\n\nsafe **text**")

	res, fieldErrs, err := svc.Publish(ctx, publish.Input{
		DocID:       docID,
		AuthorID:    author,
		Title:       "How to deploy LookupKB",
		Description: "A short but sufficient description of the deployment steps.",
		Tags:        []string{"Ops", "deploy"},
	})
	require.NoError(t, err)
	require.Empty(t, fieldErrs)
	require.Equal(t, dbgen.DocumentStatusPublished, res.Document.Status)

	doc, err := q.GetOwnedDocument(ctx, dbgen.GetOwnedDocumentParams{ID: docID, AuthorID: author})
	require.NoError(t, err)
	require.Equal(t, dbgen.DocumentStatusPublished, doc.Status)
	require.True(t, doc.PublishedAt.Valid, "published_at set on first publish")
	require.NotNil(t, doc.RenderedHtml)
	require.NotContains(t, *doc.RenderedHtml, "<script", "rendered HTML is sanitized")
	// Slug is title-derived, no longer the draft placeholder.
	require.Regexp(t, `^how-to-deploy-lookupkb-[0-9a-f]+$`, doc.Slug)

	// Live tags applied with usage_count; audit + index-outbox rows.
	require.Equal(t, 2, countRows(t, ctx, pool, "SELECT count(*) FROM document_tags WHERE document_id=$1", docID))
	require.Equal(t, 1, tagUsage(t, ctx, pool, "ops"))
	require.Equal(t, 1, tagUsage(t, ctx, pool, "deploy"))
	require.Equal(t, 1, countRows(t, ctx, pool,
		"SELECT count(*) FROM audit_log WHERE action='document.publish' AND target_id=$1", uuidStr(docID)))
	require.Equal(t, 1, countRows(t, ctx, pool,
		"SELECT count(*) FROM index_outbox WHERE document_id=$1 AND op='upsert'", docID))
}

// TestPublishFlow_RePublish covers AC-5.5: a re-publish overwrites the content,
// leaves published_at and the slug unchanged, and enqueues another index upsert.
func TestPublishFlow_RePublish(t *testing.T) {
	ctx := context.Background()
	pool, author := newFixture(t, ctx)
	svc := newPublishService(pool)
	q := dbgen.New(pool)

	docID := newDraftWithSource(t, ctx, pool, author, "draft-repub", "# v1")
	_, _, err := svc.Publish(ctx, publish.Input{
		DocID: docID, AuthorID: author,
		Title: "First title", Description: "First description, long enough.", Tags: []string{"alpha"},
	})
	require.NoError(t, err)

	before, err := q.GetOwnedDocument(ctx, dbgen.GetOwnedDocumentParams{ID: docID, AuthorID: author})
	require.NoError(t, err)

	res2, fieldErrs, err := svc.Publish(ctx, publish.Input{
		DocID: docID, AuthorID: author,
		Title: "Second title", Description: "Second description, also long enough.", Tags: []string{"alpha", "beta"},
	})
	require.NoError(t, err)
	require.Empty(t, fieldErrs)
	require.Equal(t, dbgen.DocumentStatusPublished, res2.Document.Status)

	after, err := q.GetOwnedDocument(ctx, dbgen.GetOwnedDocumentParams{ID: docID, AuthorID: author})
	require.NoError(t, err)
	require.Equal(t, before.PublishedAt.Time, after.PublishedAt.Time, "published_at is not clobbered on re-publish")
	require.Equal(t, before.Slug, after.Slug, "slug stays stable across re-publishes")

	require.Equal(t, 2, countRows(t, ctx, pool, "SELECT count(*) FROM index_outbox WHERE document_id=$1 AND op='upsert'", docID))
	// beta newly added; alpha unchanged (idempotent).
	require.Equal(t, 1, tagUsage(t, ctx, pool, "alpha"))
	require.Equal(t, 1, tagUsage(t, ctx, pool, "beta"))
}

// TestPublishFlow_InvalidMetadataNoStateChange covers AC-5.2: a metadata violation
// returns per-field errors and leaves the document a draft — no version, no audit,
// no index op.
func TestPublishFlow_InvalidMetadataNoStateChange(t *testing.T) {
	ctx := context.Background()
	pool, author := newFixture(t, ctx)
	svc := newPublishService(pool)
	q := dbgen.New(pool)

	docID := newDraftWithSource(t, ctx, pool, author, "draft-invalid", "# draft")

	res, fieldErrs, err := svc.Publish(ctx, publish.Input{
		DocID: docID, AuthorID: author,
		Title: "x", Description: "short", Tags: nil, // all three invalid
	})
	require.NoError(t, err)
	require.Nil(t, res)
	require.NotEmpty(t, fieldErrs)

	doc, err := q.GetOwnedDocument(ctx, dbgen.GetOwnedDocumentParams{ID: docID, AuthorID: author})
	require.NoError(t, err)
	require.Equal(t, dbgen.DocumentStatusDraft, doc.Status, "document stays a draft")
	require.False(t, doc.PublishedAt.Valid)
	require.Zero(t, countRows(t, ctx, pool, "SELECT count(*) FROM audit_log WHERE target_id=$1", uuidStr(docID)))
	require.Zero(t, countRows(t, ctx, pool, "SELECT count(*) FROM index_outbox WHERE document_id=$1", docID))
}

// TestPublishFlow_SettingsLimitEnforced covers AC-10.4: a limit change stored in the
// settings table is honored on the next publish attempt, no redeploy.
func TestPublishFlow_SettingsLimitEnforced(t *testing.T) {
	ctx := context.Background()
	pool, author := newFixture(t, ctx)
	svc := newPublishService(pool)

	// Tighten the title max to 5 chars; a previously-valid title now violates it.
	_, err := pool.Exec(ctx,
		`INSERT INTO settings (key, value) VALUES ('title.limits', '{"min":3,"max":5}')`)
	require.NoError(t, err)

	docID := newDraftWithSource(t, ctx, pool, author, "draft-limit", "# draft")
	_, fieldErrs, err := svc.Publish(ctx, publish.Input{
		DocID: docID, AuthorID: author,
		Title: "This title is far too long now", Description: "A sufficiently long description.", Tags: []string{"ok"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, fieldErrs)
	var sawTitle bool
	for _, fe := range fieldErrs {
		if fe.Field == publish.FieldTitle {
			sawTitle = true
		}
	}
	require.True(t, sawTitle, "the tightened title limit is enforced")
}

// TestPublishFlow_Unpublish covers AC-5.6: unpublish reverts to author-only status,
// retains published_at, enqueues an index delete, drops live
// tag counts, and writes an audit row.
func TestPublishFlow_Unpublish(t *testing.T) {
	ctx := context.Background()
	pool, author := newFixture(t, ctx)
	svc := newPublishService(pool)
	q := dbgen.New(pool)

	docID := newDraftWithSource(t, ctx, pool, author, "draft-unpub", "# doc")
	_, _, err := svc.Publish(ctx, publish.Input{
		DocID: docID, AuthorID: author,
		Title: "A publishable title", Description: "A sufficiently long description here.", Tags: []string{"alpha"},
	})
	require.NoError(t, err)
	require.Equal(t, 1, tagUsage(t, ctx, pool, "alpha"))

	res, err := svc.Unpublish(ctx, docID, author)
	require.NoError(t, err)
	require.Equal(t, dbgen.DocumentStatusUnpublished, res.Document.Status)

	doc, err := q.GetOwnedDocument(ctx, dbgen.GetOwnedDocumentParams{ID: docID, AuthorID: author})
	require.NoError(t, err)
	require.Equal(t, dbgen.DocumentStatusUnpublished, doc.Status)
	require.True(t, doc.PublishedAt.Valid, "published_at retained on unpublish")

	require.Equal(t, 0, tagUsage(t, ctx, pool, "alpha"), "unpublished doc stops counting toward tag popularity")
	require.Equal(t, 1, countRows(t, ctx, pool, "SELECT count(*) FROM index_outbox WHERE document_id=$1 AND op='delete'", docID))
	require.Equal(t, 1, countRows(t, ctx, pool,
		"SELECT count(*) FROM audit_log WHERE action='document.unpublish' AND target_id=$1", uuidStr(docID)))
}

// TestPublishFlow_CrossAuthorIsNotFound proves owner scoping: another user's publish
// attempt is ErrNotFound, never a cross-author publish.
func TestPublishFlow_CrossAuthorIsNotFound(t *testing.T) {
	ctx := context.Background()
	pool, author := newFixture(t, ctx)
	svc := newPublishService(pool)

	other, err := dbgen.New(pool).CreateUser(ctx, dbgen.CreateUserParams{Email: "other-sub@example.com", PasswordHash: "x"})
	require.NoError(t, err)

	docID := newDraftWithSource(t, ctx, pool, author, "draft-owned", "# doc")
	_, _, err = svc.Publish(ctx, publish.Input{
		DocID: docID, AuthorID: other.ID,
		Title: "A publishable title", Description: "A sufficiently long description here.", Tags: []string{"alpha"},
	})
	require.ErrorIs(t, err, publish.ErrNotFound)
}
