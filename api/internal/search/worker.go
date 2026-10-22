package search

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// Polling every second keeps index freshness within budget without LISTEN/NOTIFY.
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
	GetIndexProjections(ctx context.Context, ids []pgtype.UUID) ([]dbgen.GetIndexProjectionsRow, error)
	EnqueueAllPublishedUpserts(ctx context.Context) (int64, error)
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
	queued, err := w.store.EnqueueAllPublishedUpserts(ctx)
	if err != nil {
		return fmt.Errorf("enqueue bootstrap ops: %w", err)
	}
	if queued > 0 {
		w.log.InfoContext(ctx, "search index empty, queued published documents", "count", queued)
	}
	return nil
}

// processBatch claims and applies one batch, returning how many ops it claimed.
// Upserts share one projection query and one Meilisearch call. A failing op is
// recorded on its own row; the batch error is only returned when every op failed
// (Meilisearch is likely down), which slows the poll loop.
func (w *Worker) processBatch(ctx context.Context) (int, error) {
	ops, err := w.store.ClaimIndexOps(ctx, batchSize)
	if err != nil {
		return 0, fmt.Errorf("claim ops: %w", err)
	}
	if len(ops) == 0 {
		return 0, nil
	}
	results := w.apply(ctx, ops)

	failed := 0
	var lastErr error
	for i, op := range ops {
		if err := results[i]; err != nil {
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
	if failed == len(ops) {
		return len(ops), lastErr
	}
	return len(ops), nil
}

// apply pushes the ops to Meilisearch and returns one result per op. An upsert
// for a document that is no longer published becomes a delete so the index keeps
// no stale copy.
func (w *Worker) apply(ctx context.Context, ops []dbgen.ClaimIndexOpsRow) []error {
	results := make([]error, len(ops))

	var ids []pgtype.UUID
	for _, op := range ops {
		if op.Op != "delete" {
			ids = append(ids, op.DocumentID)
		}
	}
	rows := map[pgtype.UUID]dbgen.GetIndexProjectionsRow{}
	var loadErr error
	if len(ids) > 0 {
		list, err := w.store.GetIndexProjections(ctx, ids)
		if err != nil {
			loadErr = fmt.Errorf("load projections: %w", err)
		}
		for _, r := range list {
			rows[r.ID] = r
		}
	}

	var docs []IndexDoc
	var upserted []int
	queued := map[pgtype.UUID]bool{}
	for i, op := range ops {
		id := uuid.UUID(op.DocumentID.Bytes).String()
		if op.Op == "delete" {
			results[i] = w.client.Delete(ctx, id)
			continue
		}
		if loadErr != nil {
			results[i] = loadErr
			continue
		}
		row, ok := rows[op.DocumentID]
		if !ok {
			results[i] = w.client.Delete(ctx, id)
			continue
		}
		upserted = append(upserted, i)
		if !queued[op.DocumentID] {
			queued[op.DocumentID] = true
			docs = append(docs, Projection(row))
		}
	}
	if len(docs) > 0 {
		err := w.client.Upsert(ctx, docs...)
		for _, i := range upserted {
			results[i] = err
		}
	}
	return results
}

// Projection maps a published document row to its index document.
func Projection(r dbgen.GetIndexProjectionsRow) IndexDoc {
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
