//go:build integration

package db_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/Learistocats/LookupKB/api/internal/publish"
)

// TestIndexOutboxLifecycle covers the worker's queries: claim moves pending ops to
// processing (and never hands the same op out twice), fail retries until the
// attempt budget is spent, complete marks done, requeue recovers crashed claims.
func TestIndexOutboxLifecycle(t *testing.T) {
	ctx := context.Background()
	pool, author := newFixture(t, ctx)
	q := dbgen.New(pool)
	doc := newDoc(t, ctx, pool, author, "outbox-doc")

	require.NoError(t, q.EnqueueIndexOp(ctx, dbgen.EnqueueIndexOpParams{DocumentID: doc, Op: "upsert"}))
	require.NoError(t, q.EnqueueIndexOp(ctx, dbgen.EnqueueIndexOpParams{DocumentID: doc, Op: "delete"}))

	claimed, err := q.ClaimIndexOps(ctx, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 2)
	again, err := q.ClaimIndexOps(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, again, "claimed ops are not handed out twice")

	msg := "meili down"
	first, second := claimed[0], claimed[1]
	require.NoError(t, q.FailIndexOp(ctx, dbgen.FailIndexOpParams{ID: first.ID, LastError: &msg, MaxAttempts: 2}))
	require.Equal(t, 1, countRows(t, ctx, pool, "SELECT count(*) FROM index_outbox WHERE id=$1 AND status='pending' AND attempts=1", first.ID))

	retry, err := q.ClaimIndexOps(ctx, 10)
	require.NoError(t, err)
	require.Len(t, retry, 1)
	require.NoError(t, q.FailIndexOp(ctx, dbgen.FailIndexOpParams{ID: first.ID, LastError: &msg, MaxAttempts: 2}))
	require.Equal(t, 1, countRows(t, ctx, pool, "SELECT count(*) FROM index_outbox WHERE id=$1 AND status='failed'", first.ID))

	require.NoError(t, q.CompleteIndexOp(ctx, second.ID))
	require.Equal(t, 1, countRows(t, ctx, pool, "SELECT count(*) FROM index_outbox WHERE id=$1 AND status='done' AND processed_at IS NOT NULL", second.ID))

	require.NoError(t, q.EnqueueIndexOp(ctx, dbgen.EnqueueIndexOpParams{DocumentID: doc, Op: "upsert"}))
	_, err = q.ClaimIndexOps(ctx, 10)
	require.NoError(t, err)
	require.NoError(t, q.RequeueStaleIndexOps(ctx))
	require.Equal(t, 0, countRows(t, ctx, pool, "SELECT count(*) FROM index_outbox WHERE status='processing'"))
}

// TestListMyDocumentsAndProjection checks the history ordering (drafts first) and
// that the index projection exists only for published documents.
func TestListMyDocumentsAndProjection(t *testing.T) {
	ctx := context.Background()
	pool, author := newFixture(t, ctx)
	q := dbgen.New(pool)
	svc := newPublishService(pool)

	published := newDraftWithSource(t, ctx, pool, author, "pub", "# Kubernetes\n\nRun **kubectl**.")
	_, fieldErrs, err := svc.Publish(ctx, publish.Input{
		DocID: published, AuthorID: author,
		Title: "Kubernetes runbook", Description: "How we operate our clusters day to day.", Tags: []string{"ops"},
	})
	require.NoError(t, err)
	require.Empty(t, fieldErrs)
	draft := newDoc(t, ctx, pool, author, "a-draft")

	projs, err := q.GetIndexProjections(ctx, []pgtype.UUID{published, draft})
	require.NoError(t, err)
	require.Len(t, projs, 1, "drafts have no index projection")
	proj := projs[0]

	n, err := q.EnqueueAllPublishedUpserts(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, n, "bootstrap queues published documents only")
	require.Equal(t, []string{"ops"}, proj.Tags)
	require.Equal(t, "author", proj.Author, "falls back to the email's local part, never the full email")
	require.Contains(t, *proj.RenderedHtml, "<strong>kubectl</strong>")

	// Saving a published entry edits the working copy only.
	src := "# Edited"
	_, err = q.SaveDraft(ctx, dbgen.SaveDraftParams{ID: published, AuthorID: author, SourceMd: &src})
	require.NoError(t, err)
	projs, err = q.GetIndexProjections(ctx, []pgtype.UUID{published})
	require.NoError(t, err)
	require.Len(t, projs, 1)
	require.Contains(t, *projs[0].RenderedHtml, "kubectl", "the published HTML is unchanged until re-publish")

	// The published entry is now the most recently edited, yet the draft stays on top.
	rows, err := q.ListMyDocuments(ctx, author)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, draft, rows[0].ID, "drafts are pinned first even when older edits exist")
	require.Equal(t, published, rows[1].ID)

}
