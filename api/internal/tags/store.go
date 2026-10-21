package tags

import (
	"context"
	"fmt"

	"github.com/Learistocats/LookupKB/api/internal/access"
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
//
// Protected tags the caller is not granted are never suggested (LOO-33): the
// caller's access.Policy supplies their ids and the query excludes them BEFORE
// the LIMIT, so hiding them never shortens the list. Each row carries
// IsProtected so the publish dialog can say the entry will be restricted.
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

// TagWriter is the narrow, transaction-bound query surface ApplyDocumentTags needs
// — exactly the tag-membership + usage_count statements, nothing else. Both
// *dbgen.Queries (via WithTx) and a test fake satisfy it, and it lets the publish
// gate (LOO-14) run the tag apply INSIDE its own publish transaction rather than a
// separate one, so a rolled-back publish never leaves usage_count incremented
// (mirrors versions.VersionWriter / versions.CreateSnapshot).
type TagWriter interface {
	ListDocumentTags(ctx context.Context, documentID pgtype.UUID) ([]dbgen.ListDocumentTagsRow, error)
	RemoveDocumentTag(ctx context.Context, arg dbgen.RemoveDocumentTagParams) error
	DecrementTagUsage(ctx context.Context, id pgtype.UUID) error
	UpsertTag(ctx context.Context, arg dbgen.UpsertTagParams) (dbgen.Tag, error)
	AddDocumentTag(ctx context.Context, arg dbgen.AddDocumentTagParams) error
	IncrementTagUsage(ctx context.Context, id pgtype.UUID) error
}

// ApplyDocumentTags replaces a document's tag set with the normalized `raw` tags
// and keeps each tag's usage_count in sync, using the caller-provided
// transaction-bound queries so it composes into a larger transaction (the publish
// gate, LOO-14) as an all-or-nothing unit. It is idempotent (re-applying the same
// set is a no-op for counts) and passing an empty/nil set detaches all tags.
//
// usage_count therefore tracks LIVE published usage: publishing a tag increments
// it, unpublishing or dropping the tag on edit decrements it (clamped at zero).
//
// The caller owns the transaction boundary (Begin/Commit); ApplyDocumentTags never
// commits — a failure returns an error and the caller's deferred Rollback undoes it.
func ApplyDocumentTags(ctx context.Context, q TagWriter, docID pgtype.UUID, raw []string) error {
	// Normalize + de-dupe the desired set once, up front.
	want := make(map[string]struct{}, len(raw))
	for _, r := range raw {
		if slug := Normalize(r); slug != "" {
			want[slug] = struct{}{}
		}
	}

	// Current membership is the diff source.
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
			continue // already attached — leave count untouched (idempotent).
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

// SetDocumentTags is the standalone wrapper over ApplyDocumentTags: it owns a
// transaction (pool Begin → WithTx → deferred Rollback → Commit) for callers
// applying tags outside a larger transaction. The publish gate calls
// ApplyDocumentTags directly on its own transaction instead.
func (s *Store) SetDocumentTags(ctx context.Context, docID pgtype.UUID, raw []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tag tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after Commit; safe rollback on any early return

	if err := ApplyDocumentTags(ctx, s.q.WithTx(tx), docID, raw); err != nil {
		return err
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
