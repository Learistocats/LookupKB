package search

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Worker tuning. Polling every second keeps index freshness under the 2 s
// budget (05-search-and-indexing.md §5) without LISTEN/NOTIFY plumbing.
const (
	pollInterval = time.Second
	maxBackoff   = 30 * time.Second
	batchSize    = 50
	maxAttempts  = 5
)

// Store is the subset of generated queries the worker needs.
type Store interface {
	ClaimIndexOps(ctx context.Context, limit int32) ([]dbgen.ClaimIndexOpsRow, error)
	CompleteIndexOp(ctx context.Context, id int64) error
	FailIndexOp(ctx context.Context, arg dbgen.FailIndexOpParams) error
	RequeueStaleIndexOps(ctx context.Context) error
	GetIndexProjection(ctx context.Context, id pgtype.UUID) (dbgen.GetIndexProjectionRow, error)
	ListPublishedDocumentIDs(ctx context.Context) ([]pgtype.UUID, error)
	EnqueueIndexOp(ctx context.Context, arg dbgen.EnqueueIndexOpParams) error
}

// Worker drains index_outbox into Meilisearch.
type Worker struct {
	store  Store
	client *Client
	log    *slog.Logger
}

// NewWorker builds the outbox worker.
func NewWorker(store Store, client *Client, log *slog.Logger) *Worker {
	return &Worker{store: store, client: client, log: log}
}

// Run prepares the index, then processes the outbox until ctx is canceled.
// Preparation retries with backoff so a slow Meilisearch start never kills the API.
func (w *Worker) Run(ctx context.Context) {
	backoff := pollInterval
	for {
		err := w.prepare(ctx)
		if err == nil {
			break
		}
		w.log.WarnContext(ctx, "search index not ready, retrying", "error", err.Error(), "retry_in", backoff.String())
		if !sleep(ctx, backoff) {
			return
		}
		backoff = min(backoff*2, maxBackoff)
	}
	w.log.InfoContext(ctx, "search index worker started")

	backoff = pollInterval
	for {
		n, err := w.processBatch(ctx)
		if ctx.Err() != nil {
			return
		}
		wait := pollInterval
		switch {
		case err != nil:
			w.log.WarnContext(ctx, "search index batch failed", "error", err.Error())
			wait, backoff = backoff, min(backoff*2, maxBackoff)
		case n > 0:
			wait, backoff = 0, pollInterval // keep draining
		default:
			backoff = pollInterval
		}
		if wait > 0 && !sleep(ctx, wait) {
			return
		}
	}
}

// prepare ensures index + settings, recovers ops orphaned by a crash, and
// bootstraps an empty index from the published documents in Postgres.
func (w *Worker) prepare(ctx context.Context) error {
	if err := w.client.EnsureIndex(ctx); err != nil {
		return err
	}
	if err := w.store.RequeueStaleIndexOps(ctx); err != nil {
		return fmt.Errorf("requeue stale ops: %w", err)
	}
	count, err := w.client.DocumentCount(ctx)
	if err != nil {
		return fmt.Errorf("index stats: %w", err)
	}
	if count > 0 {
		return nil
	}
	ids, err := w.store.ListPublishedDocumentIDs(ctx)
	if err != nil {
		return fmt.Errorf("list published: %w", err)
	}
	for _, id := range ids {
		if err := w.store.EnqueueIndexOp(ctx, dbgen.EnqueueIndexOpParams{DocumentID: id, Op: "upsert"}); err != nil {
			return fmt.Errorf("enqueue bootstrap op: %w", err)
		}
	}
	if len(ids) > 0 {
		w.log.InfoContext(ctx, "search index empty, queued published documents", "count", len(ids))
	}
	return nil
}

// processBatch claims and applies one batch, returning how many ops it claimed.
// A failing op is recorded on its own row; the batch error is only returned when
// every op failed (Meilisearch is likely down), which slows the poll loop.
func (w *Worker) processBatch(ctx context.Context) (int, error) {
	ops, err := w.store.ClaimIndexOps(ctx, batchSize)
	if err != nil {
		return 0, fmt.Errorf("claim ops: %w", err)
	}
	failed := 0
	var lastErr error
	for _, op := range ops {
		if err := w.apply(ctx, op); err != nil {
			failed++
			lastErr = err
			msg := err.Error()
			if ferr := w.store.FailIndexOp(ctx, dbgen.FailIndexOpParams{ID: op.ID, LastError: &msg, MaxAttempts: maxAttempts}); ferr != nil {
				return len(ops), fmt.Errorf("record failure: %w", ferr)
			}
			w.log.WarnContext(ctx, "search index op failed", "op_id", op.ID, "op", op.Op, "attempt", op.Attempts+1, "error", msg)
			continue
		}
		if err := w.store.CompleteIndexOp(ctx, op.ID); err != nil {
			return len(ops), fmt.Errorf("complete op: %w", err)
		}
	}
	if len(ops) > 0 && failed == len(ops) {
		return len(ops), lastErr
	}
	return len(ops), nil
}

func (w *Worker) apply(ctx context.Context, op dbgen.ClaimIndexOpsRow) error {
	id := uuid.UUID(op.DocumentID.Bytes).String()
	if op.Op == "delete" {
		return w.client.Delete(ctx, id)
	}
	row, err := w.store.GetIndexProjection(ctx, op.DocumentID)
	if errors.Is(err, pgx.ErrNoRows) {
		// No longer published (unpublished/deleted since enqueue): make sure the
		// index does not keep a stale copy.
		return w.client.Delete(ctx, id)
	}
	if err != nil {
		return fmt.Errorf("load projection: %w", err)
	}
	return w.client.Upsert(ctx, Projection(row))
}

// Projection maps a published document row to its index document.
func Projection(r dbgen.GetIndexProjectionRow) IndexDoc {
	groups := make([]string, 0, len(r.AclGroupIds))
	for _, g := range r.AclGroupIds {
		groups = append(groups, uuid.UUID(g.Bytes).String())
	}
	tags := r.Tags
	if tags == nil {
		tags = []string{}
	}
	return IndexDoc{
		ID:          uuid.UUID(r.ID.Bytes).String(),
		Slug:        r.Slug,
		Title:       deref(r.Title),
		Description: deref(r.Description),
		BodyText:    TextFromHTML(deref(r.RenderedHtml)),
		Tags:        tags,
		Author:      r.Author,
		AuthorID:    uuid.UUID(r.AuthorID.Bytes).String(),
		CreatedAt:   r.CreatedAt.Time.Unix(),
		PublishedAt: r.PublishedAt.Time.Unix(),
		Access:      string(r.Access),
		ACLGroupIDs: groups,
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
