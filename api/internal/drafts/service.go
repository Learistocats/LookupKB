// Package drafts holds draft lifecycle operations that need a transaction
// spanning several tables — today, hard-deleting a draft (LOO-58). Plain
// create/save/read stay single statements behind server.DocumentStore.
package drafts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/Learistocats/LookupKB/api/internal/tags"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound means the document does not exist, is not the caller's, is
// soft-deleted, or is published (a published doc must be unpublished before it
// can be deleted). The handler maps every case to the same 404 body so a probing
// user can never tell them apart (03-data-model.md §2).
var ErrNotFound = errors.New("draft not found")

// TxQueries is the transaction-bound query surface Delete needs: the delete
// itself, the defensive index op, the audit row, and the tag-membership
// statements (so any live tag usage is decremented before document_tags
// cascades away). *dbgen.Queries (via WithTx) satisfies it; tests use a fake.
type TxQueries interface {
	tags.TagWriter
	DeleteDraft(ctx context.Context, arg dbgen.DeleteDraftParams) (dbgen.DeleteDraftRow, error)
	EnqueueIndexOp(ctx context.Context, arg dbgen.EnqueueIndexOpParams) error
	InsertAuditLog(ctx context.Context, arg dbgen.InsertAuditLogParams) error
}

// TxFunc runs fn inside one transaction: commit when fn returns nil, roll back
// otherwise.
type TxFunc func(ctx context.Context, fn func(q TxQueries) error) error

// Service performs transactional draft operations.
type Service struct {
	inTx TxFunc
}

// NewService builds a Service over the connection pool.
func NewService(pool *pgxpool.Pool) *Service {
	q := dbgen.New(pool)
	return NewServiceWithTx(func(ctx context.Context, fn func(q TxQueries) error) error {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin tx: %w", err)
		}
		defer tx.Rollback(ctx) //nolint:errcheck // no-op after Commit; safe rollback on any early return
		if err := fn(q.WithTx(tx)); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit tx: %w", err)
		}
		return nil
	})
}

// NewServiceWithTx builds a Service over a caller-supplied transaction runner
// (unit tests inject a fake one).
func NewServiceWithTx(inTx TxFunc) *Service {
	return &Service{inTx: inTx}
}

// Delete hard-deletes one of the author's own draft or unpublished documents in
// a single transaction:
//
//   - detach any tags (decrementing usage_count). Drafts never carry tags and
//     unpublish already clears them, so this is normally a no-op read — it only
//     guards the invariant so usage counts can never drift on a cascade.
//   - DELETE the row (owner + status scoped; anything else is ErrNotFound and the
//     transaction rolls back, undoing the tag step).
//   - for a previously-published ('unpublished') doc, enqueue a defensive index
//     'delete' op; the outbox row survives the hard delete (no FK since 00008).
//     A pure draft was never indexed, so nothing is enqueued (drafts never touch
//     search).
//   - append a document.delete audit row.
func (s *Service) Delete(ctx context.Context, docID, authorID pgtype.UUID) error {
	return s.inTx(ctx, func(q TxQueries) error {
		if err := tags.ApplyDocumentTags(ctx, q, docID, nil); err != nil {
			return fmt.Errorf("clear tags: %w", err)
		}

		row, err := q.DeleteDraft(ctx, dbgen.DeleteDraftParams{ID: docID, AuthorID: authorID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("delete draft: %w", err)
		}

		if row.Status == dbgen.DocumentStatusUnpublished {
			if err := q.EnqueueIndexOp(ctx, dbgen.EnqueueIndexOpParams{DocumentID: docID, Op: "delete"}); err != nil {
				return fmt.Errorf("enqueue index op: %w", err)
			}
		}

		meta, err := json.Marshal(map[string]any{"status": string(row.Status)})
		if err != nil {
			meta = []byte("{}")
		}
		targetID := uuid.UUID(docID.Bytes).String()
		if err := q.InsertAuditLog(ctx, dbgen.InsertAuditLogParams{
			ActorID:    authorID,
			Action:     "document.delete",
			TargetType: "document",
			TargetID:   &targetID,
			Metadata:   meta,
		}); err != nil {
			return fmt.Errorf("write audit log: %w", err)
		}
		return nil
	})
}
