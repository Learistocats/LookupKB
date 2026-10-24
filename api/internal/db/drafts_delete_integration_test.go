//go:build integration

package db_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	appdb "github.com/Learistocats/LookupKB/api/internal/db"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/Learistocats/LookupKB/api/internal/drafts"
	"github.com/Learistocats/LookupKB/api/internal/publish"
)

func docExists(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id pgtype.UUID) bool {
	t.Helper()
	return countRows(t, ctx, pool, "SELECT count(*) FROM documents WHERE id=$1", id) == 1
}

func publishDoc(t *testing.T, ctx context.Context, pool *pgxpool.Pool, author, docID pgtype.UUID, tags []string) {
	t.Helper()
	_, fieldErrs, err := newPublishService(pool).Publish(ctx, publish.Input{
		DocID: docID, AuthorID: author,
		Title: "A publishable title", Description: "A sufficiently long description here.", Tags: tags,
	})
	require.NoError(t, err)
	require.Empty(t, fieldErrs)
}

// TestDeleteDraft covers delete end-to-end through the real drafts service: only
// the owner's draft/unpublished rows go; missing, foreign and published ids are
// the same ErrNotFound and leave everything untouched.
func TestDeleteDraft(t *testing.T) {
	ctx := context.Background()
	pool, author := newFixture(t, ctx)
	svc := drafts.NewService(pool)

	other, err := dbgen.New(pool).CreateUser(ctx, dbgen.CreateUserParams{Email: "other@example.com", PasswordHash: "x"})
	require.NoError(t, err)

	t.Run("own draft is hard-deleted, audited, never indexed", func(t *testing.T) {
		docID := newDraftWithSource(t, ctx, pool, author, "draft-del-own", "# bye")
		require.NoError(t, svc.Delete(ctx, docID, author))

		require.False(t, docExists(t, ctx, pool, docID))
		require.Equal(t, 0, countRows(t, ctx, pool, "SELECT count(*) FROM index_outbox WHERE document_id=$1", docID),
			"drafts never touch search")
		require.Equal(t, 1, countRows(t, ctx, pool,
			"SELECT count(*) FROM audit_log WHERE action='document.delete' AND target_id=$1 AND actor_id=$2 AND metadata->>'status'='draft'",
			uuidStr(docID), author))
	})

	t.Run("missing id is not found", func(t *testing.T) {
		missing := pgtype.UUID{Bytes: uuid.New(), Valid: true}
		require.ErrorIs(t, svc.Delete(ctx, missing, author), drafts.ErrNotFound)
	})

	t.Run("other author's draft is not found and survives", func(t *testing.T) {
		docID := newDoc(t, ctx, pool, author, "draft-del-foreign")
		require.ErrorIs(t, svc.Delete(ctx, docID, other.ID), drafts.ErrNotFound)
		require.True(t, docExists(t, ctx, pool, docID))
		require.Equal(t, 0, countRows(t, ctx, pool,
			"SELECT count(*) FROM audit_log WHERE action='document.delete' AND target_id=$1", uuidStr(docID)))
	})

	t.Run("published doc is not found and keeps its tags", func(t *testing.T) {
		docID := newDraftWithSource(t, ctx, pool, author, "draft-del-pub", "# live")
		publishDoc(t, ctx, pool, author, docID, []string{"keepme"})
		outboxBefore := countRows(t, ctx, pool, "SELECT count(*) FROM index_outbox WHERE document_id=$1", docID)

		require.ErrorIs(t, svc.Delete(ctx, docID, author), drafts.ErrNotFound)
		require.True(t, docExists(t, ctx, pool, docID))
		require.Equal(t, 1, tagUsage(t, ctx, pool, "keepme"), "rolled-back delete must not touch usage_count")
		require.Equal(t, 1, countRows(t, ctx, pool, "SELECT count(*) FROM document_tags WHERE document_id=$1", docID))
		require.Equal(t, outboxBefore, countRows(t, ctx, pool, "SELECT count(*) FROM index_outbox WHERE document_id=$1", docID))
	})

	t.Run("unpublished doc is deleted with a surviving index delete op", func(t *testing.T) {
		docID := newDraftWithSource(t, ctx, pool, author, "draft-del-unpub", "# was live")
		publishDoc(t, ctx, pool, author, docID, []string{"gone"})
		_, err := newPublishService(pool).Unpublish(ctx, docID, author)
		require.NoError(t, err)

		require.NoError(t, svc.Delete(ctx, docID, author))

		require.False(t, docExists(t, ctx, pool, docID))
		require.Equal(t, 0, tagUsage(t, ctx, pool, "gone"))
		// One 'delete' from unpublish + the defensive one from delete; both outlive
		// the document row now that index_outbox has no FK (00008).
		require.Equal(t, 2, countRows(t, ctx, pool,
			"SELECT count(*) FROM index_outbox WHERE document_id=$1 AND op='delete'", docID))
		require.Equal(t, 1, countRows(t, ctx, pool,
			"SELECT count(*) FROM audit_log WHERE action='document.delete' AND target_id=$1 AND metadata->>'status'='unpublished'",
			uuidStr(docID)))
	})
}

// TestOutboxFKMigration proves 00008 applies and reverses: with it, an outbox row
// may reference a vanished document; rolling it back removes such orphans and
// restores the cascading FK.
func TestOutboxFKMigration(t *testing.T) {
	ctx := context.Background()
	pool, author := newFixture(t, ctx)
	dsn := pool.Config().ConnString()

	const fkCount = `SELECT count(*) FROM pg_constraint WHERE conname='index_outbox_document_id_fkey'`
	require.Equal(t, 0, countRows(t, ctx, pool, fkCount), "00008 drops the FK")

	kept := newDoc(t, ctx, pool, author, "draft-fk-kept")
	q := dbgen.New(pool)
	require.NoError(t, q.EnqueueIndexOp(ctx, dbgen.EnqueueIndexOpParams{DocumentID: kept, Op: "upsert"}))
	orphan := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	require.NoError(t, q.EnqueueIndexOp(ctx, dbgen.EnqueueIndexOpParams{DocumentID: orphan, Op: "delete"}),
		"an outbox row must not need its document")

	sqlDB, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()

	// Step down until 00008 is reverted (later migrations, if any, go first).
	for {
		v, err := goose.GetDBVersion(sqlDB)
		require.NoError(t, err)
		if v < 8 {
			break
		}
		require.NoError(t, appdb.RunMigrationsDB(ctx, sqlDB, "down"))
	}

	require.Equal(t, 1, countRows(t, ctx, pool, fkCount), "down restores the FK")
	require.Equal(t, 0, countRows(t, ctx, pool, "SELECT count(*) FROM index_outbox WHERE document_id=$1", orphan),
		"down drops orphaned outbox rows")
	require.Equal(t, 1, countRows(t, ctx, pool, "SELECT count(*) FROM index_outbox WHERE document_id=$1", kept))

	require.NoError(t, appdb.RunMigrationsDB(ctx, sqlDB, "up"))
	require.Equal(t, 0, countRows(t, ctx, pool, fkCount))
}
