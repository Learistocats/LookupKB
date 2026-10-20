package drafts

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

// fakeTx records the statements Delete issues; committed reports whether the
// fake transaction runner would have committed.
type fakeTx struct {
	deleteRow dbgen.DeleteDraftRow
	deleteErr error
	docTags   []dbgen.ListDocumentTagsRow

	deleteArg   dbgen.DeleteDraftParams
	enqueued    []dbgen.EnqueueIndexOpParams
	audits      []dbgen.InsertAuditLogParams
	removed     []dbgen.RemoveDocumentTagParams
	decremented []pgtype.UUID
}

func (f *fakeTx) ListDocumentTags(context.Context, pgtype.UUID) ([]dbgen.ListDocumentTagsRow, error) {
	return f.docTags, nil
}
func (f *fakeTx) RemoveDocumentTag(_ context.Context, arg dbgen.RemoveDocumentTagParams) error {
	f.removed = append(f.removed, arg)
	return nil
}
func (f *fakeTx) DecrementTagUsage(_ context.Context, id pgtype.UUID) error {
	f.decremented = append(f.decremented, id)
	return nil
}
func (f *fakeTx) UpsertTag(context.Context, dbgen.UpsertTagParams) (dbgen.Tag, error) {
	return dbgen.Tag{}, errors.New("unexpected UpsertTag")
}
func (f *fakeTx) AddDocumentTag(context.Context, dbgen.AddDocumentTagParams) error {
	return errors.New("unexpected AddDocumentTag")
}
func (f *fakeTx) IncrementTagUsage(context.Context, pgtype.UUID) error {
	return errors.New("unexpected IncrementTagUsage")
}
func (f *fakeTx) DeleteDraft(_ context.Context, arg dbgen.DeleteDraftParams) (dbgen.DeleteDraftRow, error) {
	f.deleteArg = arg
	return f.deleteRow, f.deleteErr
}
func (f *fakeTx) EnqueueIndexOp(_ context.Context, arg dbgen.EnqueueIndexOpParams) error {
	f.enqueued = append(f.enqueued, arg)
	return nil
}
func (f *fakeTx) InsertAuditLog(_ context.Context, arg dbgen.InsertAuditLogParams) error {
	f.audits = append(f.audits, arg)
	return nil
}

func newFakeService(f *fakeTx, committed *bool) *Service {
	return NewServiceWithTx(func(_ context.Context, fn func(q TxQueries) error) error {
		if err := fn(f); err != nil {
			return err
		}
		*committed = true
		return nil
	})
}

func pg(u uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: u, Valid: true} }

func TestDelete_DraftAuditsWithoutIndexOp(t *testing.T) {
	doc, author := uuid.New(), uuid.New()
	f := &fakeTx{deleteRow: dbgen.DeleteDraftRow{ID: pg(doc), Status: dbgen.DocumentStatusDraft}}
	var committed bool

	require.NoError(t, newFakeService(f, &committed).Delete(context.Background(), pg(doc), pg(author)))

	require.True(t, committed)
	require.Equal(t, dbgen.DeleteDraftParams{ID: pg(doc), AuthorID: pg(author)}, f.deleteArg)
	require.Empty(t, f.enqueued, "a pure draft was never indexed: no outbox row")
	require.Len(t, f.audits, 1)
	a := f.audits[0]
	require.Equal(t, "document.delete", a.Action)
	require.Equal(t, "document", a.TargetType)
	require.Equal(t, pg(author), a.ActorID)
	require.Equal(t, doc.String(), *a.TargetID)
	var meta map[string]any
	require.NoError(t, json.Unmarshal(a.Metadata, &meta))
	require.Equal(t, "draft", meta["status"])
}

func TestDelete_UnpublishedEnqueuesDefensiveIndexDelete(t *testing.T) {
	doc, author := uuid.New(), uuid.New()
	f := &fakeTx{deleteRow: dbgen.DeleteDraftRow{ID: pg(doc), Status: dbgen.DocumentStatusUnpublished}}
	var committed bool

	require.NoError(t, newFakeService(f, &committed).Delete(context.Background(), pg(doc), pg(author)))

	require.True(t, committed)
	require.Equal(t, []dbgen.EnqueueIndexOpParams{{DocumentID: pg(doc), Op: "delete"}}, f.enqueued)
	require.Len(t, f.audits, 1)
}

func TestDelete_NotFoundRollsBack(t *testing.T) {
	f := &fakeTx{deleteErr: pgx.ErrNoRows}
	var committed bool

	err := newFakeService(f, &committed).Delete(context.Background(), pg(uuid.New()), pg(uuid.New()))

	require.ErrorIs(t, err, ErrNotFound)
	require.False(t, committed)
	require.Empty(t, f.enqueued)
	require.Empty(t, f.audits)
}

func TestDelete_DetachesLingeringTagsBeforeDelete(t *testing.T) {
	doc := uuid.New()
	tagID := pg(uuid.New())
	f := &fakeTx{
		deleteRow: dbgen.DeleteDraftRow{ID: pg(doc), Status: dbgen.DocumentStatusDraft},
		docTags:   []dbgen.ListDocumentTagsRow{{ID: tagID, Slug: "go"}},
	}
	var committed bool

	require.NoError(t, newFakeService(f, &committed).Delete(context.Background(), pg(doc), pg(uuid.New())))

	require.Equal(t, []pgtype.UUID{tagID}, f.decremented, "usage_count must not drift when document_tags cascades")
	require.Len(t, f.removed, 1)
}
