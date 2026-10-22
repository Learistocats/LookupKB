package tags

import (
	"context"
	"fmt"

	"github.com/Learistocats/LookupKB/api/internal/access"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store serves the tag autocomplete.
type Store struct {
	q *dbgen.Queries
}

// New builds a Store over the connection pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{q: dbgen.New(pool)}
}

// searchLimits bound the autocomplete result set.
const (
	defaultSearchLimit int32 = 10
	maxSearchLimit     int32 = 20
)

// SearchTags returns tags whose slug contains the normalized query, most-used
// first. The query is normalized like tag creation so "My T" matches "my-tag"; an
// empty query returns the most-used tags. limit is clamped to [1, maxSearchLimit].
//
// Protected tags the caller is not granted are excluded in SQL before the LIMIT,
// so hiding them never shortens the list.
func (s *Store) SearchTags(ctx context.Context, rawQuery string, limit int32, caller access.Caller) ([]dbgen.SearchTagsRow, error) {
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}
	// Normalize strips LIKE metacharacters, so wrapping in % is injection-safe.
	pattern := "%" + Normalize(rawQuery) + "%"
	policy, err := access.Load(ctx, s.q, caller)
	if err != nil {
		return nil, err
	}
	return s.q.SearchTags(ctx, dbgen.SearchTagsParams{Pattern: pattern, ExcludedIds: policy.DeniedIDs(), MaxRows: limit})
}

// TagWriter is the transaction-bound query surface ApplyDocumentTags needs, so the
// apply runs inside the caller's transaction and a rollback never leaves
// usage_count incremented.
type TagWriter interface {
	ListDocumentTags(ctx context.Context, documentID pgtype.UUID) ([]dbgen.ListDocumentTagsRow, error)
	RemoveDocumentTag(ctx context.Context, arg dbgen.RemoveDocumentTagParams) error
	DecrementTagUsage(ctx context.Context, id pgtype.UUID) error
	UpsertTag(ctx context.Context, arg dbgen.UpsertTagParams) (dbgen.Tag, error)
	AddDocumentTag(ctx context.Context, arg dbgen.AddDocumentTagParams) error
	IncrementTagUsage(ctx context.Context, id pgtype.UUID) error
}

// ApplyDocumentTags replaces a document's tag set with the normalized `raw` tags
// and keeps each tag's usage_count in sync (live published usage). It is
// idempotent and a nil set detaches all tags. The caller owns the transaction:
// this never commits, and a failure is undone by the caller's rollback.
func ApplyDocumentTags(ctx context.Context, q TagWriter, docID pgtype.UUID, raw []string) error {
	want := make(map[string]struct{}, len(raw))
	for _, r := range raw {
		if slug := Normalize(r); slug != "" {
			want[slug] = struct{}{}
		}
	}

	current, err := q.ListDocumentTags(ctx, docID)
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
		if err := q.RemoveDocumentTag(ctx, dbgen.RemoveDocumentTagParams{DocumentID: docID, TagID: id}); err != nil {
			return fmt.Errorf("remove tag %q: %w", slug, err)
		}
		if err := q.DecrementTagUsage(ctx, id); err != nil {
			return fmt.Errorf("decrement tag %q: %w", slug, err)
		}
	}

	// Add newly-wanted tags: upsert vocabulary row, attach, increment.
	for slug := range want {
		if _, exists := have[slug]; exists {
			continue // already attached: leave the count untouched
		}
		tag, err := q.UpsertTag(ctx, dbgen.UpsertTagParams{Slug: slug, Label: slug})
		if err != nil {
			return fmt.Errorf("upsert tag %q: %w", slug, err)
		}
		if err := q.AddDocumentTag(ctx, dbgen.AddDocumentTagParams{DocumentID: docID, TagID: tag.ID}); err != nil {
			return fmt.Errorf("attach tag %q: %w", slug, err)
		}
		if err := q.IncrementTagUsage(ctx, tag.ID); err != nil {
			return fmt.Errorf("increment tag %q: %w", slug, err)
		}
	}

	return nil
}
