package tags

import (
	"context"
	"fmt"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store is the tag vocabulary's data access, wrapping the pgx pool so it can run
// the multi-statement tag-apply atomically in a transaction — the DocumentStore
// seam uses a bare *dbgen.Queries, but maintaining usage_count alongside
// document_tags membership needs a real transaction, so this holds the pool.
type Store struct {
	pool *pgxpool.Pool
	q    *dbgen.Queries
}

// New builds a Store over the connection pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: dbgen.New(pool)}
}

// GetSetting exposes the settings read so *Store satisfies SettingGetter (for
// LoadLimits) without a separate wiring path.
func (s *Store) GetSetting(ctx context.Context, key string) ([]byte, error) {
	return s.q.GetSetting(ctx, key)
}

// searchLimits bound the autocomplete result set.
const (
	defaultSearchLimit int32 = 10
	maxSearchLimit     int32 = 20
)

// SearchTags returns tags whose slug contains the normalized query, most-used
// first, for the autocomplete dropdown (AC-6.1). The query is normalized with the
// same rule as tag creation so a partial "My T" matches "my-tag"; an empty query
// returns the most-used tags. limit is clamped to [1, maxSearchLimit].
func (s *Store) SearchTags(ctx context.Context, rawQuery string, limit int32) ([]dbgen.Tag, error) {
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}
	// Normalize strips LIKE metacharacters, so wrapping in % is injection-safe.
	pattern := "%" + Normalize(rawQuery) + "%"
	return s.q.SearchTags(ctx, dbgen.SearchTagsParams{Slug: pattern, Limit: limit})
}

// SetDocumentTags replaces a document's tag set with the normalized `raw` tags and
// keeps each tag's usage_count in sync — the reusable apply the publish gate
// (LOO-14) and re-publish path (LOO-17) call inside their transaction. It is
// atomic (all-or-nothing) and idempotent (re-applying the same set is a no-op for
// counts). Passing an empty/nil set detaches all tags (see ClearDocumentTags).
//
// usage_count therefore tracks LIVE published usage: publishing a tag increments
// it, unpublishing or dropping the tag on edit decrements it (clamped at zero).
func (s *Store) SetDocumentTags(ctx context.Context, docID pgtype.UUID, raw []string) error {
	// Normalize + de-dupe the desired set once, up front.
	want := make(map[string]struct{}, len(raw))
	for _, r := range raw {
		if slug := Normalize(r); slug != "" {
			want[slug] = struct{}{}
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tag tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after Commit; safe rollback on any early return
	qtx := s.q.WithTx(tx)

	// Current membership is the diff source.
	current, err := qtx.ListDocumentTags(ctx, docID)
	if err != nil {
		return fmt.Errorf("list current tags: %w", err)
	}
	have := make(map[string]pgtype.UUID, len(current))
	for _, t := range current {
		have[t.Slug] = t.ID
	}

	// Remove tags no longer wanted: detach + decrement.
	for slug, id := range have {
		if _, keep := want[slug]; keep {
			continue
		}
		if err := qtx.RemoveDocumentTag(ctx, dbgen.RemoveDocumentTagParams{DocumentID: docID, TagID: id}); err != nil {
			return fmt.Errorf("remove tag %q: %w", slug, err)
		}
		if err := qtx.DecrementTagUsage(ctx, id); err != nil {
			return fmt.Errorf("decrement tag %q: %w", slug, err)
		}
	}

	// Add newly-wanted tags: upsert vocabulary row, attach, increment.
	for slug := range want {
		if _, exists := have[slug]; exists {
			continue // already attached — leave count untouched (idempotent).
		}
		tag, err := qtx.UpsertTag(ctx, dbgen.UpsertTagParams{Slug: slug, Label: slug})
		if err != nil {
			return fmt.Errorf("upsert tag %q: %w", slug, err)
		}
		if err := qtx.AddDocumentTag(ctx, dbgen.AddDocumentTagParams{DocumentID: docID, TagID: tag.ID}); err != nil {
			return fmt.Errorf("attach tag %q: %w", slug, err)
		}
		if err := qtx.IncrementTagUsage(ctx, tag.ID); err != nil {
			return fmt.Errorf("increment tag %q: %w", slug, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tag tx: %w", err)
	}
	return nil
}

// ClearDocumentTags detaches every tag from a document and decrements the affected
// usage_counts — the unpublish path, so a unpublished document stops counting
// toward tag popularity. Equivalent to SetDocumentTags with an empty set.
func (s *Store) ClearDocumentTags(ctx context.Context, docID pgtype.UUID) error {
	return s.SetDocumentTags(ctx, docID, nil)
}
