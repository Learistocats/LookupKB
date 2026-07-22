// Package versions is the published-version snapshot store (LOO-17, F9 storage).
//
// Per the product decision in 00-overview.md §8, LookupKB keeps no per-save draft
// history — only *published* versions are snapshotted. Every publish/re-publish
// appends an immutable document_versions row capturing the document exactly as it
// was published (title/description/source/rendered HTML/tags/access), and repoints
// documents.current_version_id at it (03-data-model.md §document_versions).
//
// This package is producer-only, like internal/render and internal/tags: it exposes
// the snapshot-write primitive and an internal read path, but no HTTP surface and no
// status/published_at transition — the metadata-gate publish orchestrator (LOO-14)
// composes CreateSnapshot into its publish transaction alongside the status flip.
package versions

import (
	"context"
	"fmt"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store is the version-snapshot data access. Like tags.Store it wraps the pool so
// the multi-statement snapshot write (lock → insert → repoint) can run atomically;
// the publish orchestrator can instead call CreateSnapshot directly with its own
// transaction-bound Queries so the snapshot shares the publish transaction.
type Store struct {
	pool *pgxpool.Pool
	q    *dbgen.Queries
}

// New builds a Store over the connection pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: dbgen.New(pool)}
}

// Snapshot is the fully-assembled, point-in-time input for one publish. The caller
// (LOO-14) hands in the just-validated metadata, the just-rendered HTML, the applied
// tag set and the document's access scope; every field is copied verbatim into the
// snapshot row, NOT re-read from live rows — that is what makes the snapshot immutable
// against later edits. Access/ACLGroupIDs are captured accurately now (even though the
// restricted-ACL UI is M3) so the columns aren't stubs that need backfilling later.
type Snapshot struct {
	DocumentID   pgtype.UUID
	Title        *string
	Description  *string
	SourceMD     *string
	RenderedHTML *string
	Tags         []string
	Access       dbgen.DocumentAccess
	ACLGroupIDs  []pgtype.UUID
	PublishedBy  pgtype.UUID
}

// Version is the domain view of a stored snapshot returned to callers, so they depend
// on this package rather than on dbgen directly.
type Version struct {
	ID           pgtype.UUID
	DocumentID   pgtype.UUID
	VersionNo    int32
	Title        *string
	Description  *string
	SourceMD     *string
	RenderedHTML *string
	Tags         []string
	Access       dbgen.DocumentAccess
	ACLGroupIDs  []pgtype.UUID
	PublishedBy  pgtype.UUID
	PublishedAt  pgtype.Timestamptz
}

func versionFromRow(row dbgen.DocumentVersion) Version {
	return Version{
		ID:           row.ID,
		DocumentID:   row.DocumentID,
		VersionNo:    row.VersionNo,
		Title:        row.Title,
		Description:  row.Description,
		SourceMD:     row.SourceMd,
		RenderedHTML: row.RenderedHtml,
		Tags:         row.Tags,
		Access:       row.Access,
		ACLGroupIDs:  row.AclGroupIds,
		PublishedBy:  row.PublishedBy,
		PublishedAt:  row.PublishedAt,
	}
}

// VersionWriter is the narrow, transaction-bound query surface CreateSnapshot needs.
// Both *dbgen.Queries (via WithTx) and a test fake satisfy it, so the snapshot logic
// is unit-testable without a database.
type VersionWriter interface {
	LockDocumentForPublish(ctx context.Context, id pgtype.UUID) (pgtype.UUID, error)
	CreateDocumentVersion(ctx context.Context, arg dbgen.CreateDocumentVersionParams) (dbgen.DocumentVersion, error)
	SetCurrentVersion(ctx context.Context, arg dbgen.SetCurrentVersionParams) error
}

// CreateSnapshot appends the next immutable version for a document and repoints
// current_version_id at it, using the caller-provided transaction-bound queries so it
// composes into the publish transaction (LOO-14) as an all-or-nothing unit. It:
//
//  1. locks the document row (serializes concurrent re-publishes; also verifies the
//     document exists and isn't soft-deleted — pgx.ErrNoRows otherwise),
//  2. inserts the snapshot with version_no = prior max + 1,
//  3. repoints documents.current_version_id at the new snapshot.
//
// It deliberately knows nothing about status/published_at — the publish gate owns
// that transition and runs it in the same transaction.
func CreateSnapshot(ctx context.Context, q VersionWriter, snap Snapshot) (*Version, error) {
	if _, err := q.LockDocumentForPublish(ctx, snap.DocumentID); err != nil {
		return nil, fmt.Errorf("lock document for publish: %w", err)
	}

	row, err := q.CreateDocumentVersion(ctx, dbgen.CreateDocumentVersionParams{
		DocumentID:   snap.DocumentID,
		Title:        snap.Title,
		Description:  snap.Description,
		SourceMd:     snap.SourceMD,
		RenderedHtml: snap.RenderedHTML,
		Tags:         snap.Tags,
		Access:       snap.Access,
		AclGroupIds:  snap.ACLGroupIDs,
		PublishedBy:  snap.PublishedBy,
	})
	if err != nil {
		return nil, fmt.Errorf("insert document version: %w", err)
	}

	if err := q.SetCurrentVersion(ctx, dbgen.SetCurrentVersionParams{
		ID:               snap.DocumentID,
		CurrentVersionID: row.ID,
	}); err != nil {
		return nil, fmt.Errorf("repoint current version: %w", err)
	}

	v := versionFromRow(row)
	return &v, nil
}

// Publish is a convenience that wraps CreateSnapshot in its own transaction (pool
// Begin → WithTx → deferred Rollback → Commit, exactly like tags.Store), for callers
// that publish outside a larger transaction. Once the LOO-14 orchestrator exists it
// will call CreateSnapshot directly on its own transaction instead.
func (s *Store) Publish(ctx context.Context, snap Snapshot) (*Version, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin publish tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after Commit; safe rollback on any early return

	v, err := CreateSnapshot(ctx, s.q.WithTx(tx), snap)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit publish tx: %w", err)
	}
	return v, nil
}

// GetVersions returns a document's snapshots, newest first — the minimal internal
// read path the M5 history UI builds on. It applies no access check; the caller does.
func (s *Store) GetVersions(ctx context.Context, docID pgtype.UUID) ([]Version, error) {
	rows, err := s.q.GetDocumentVersions(ctx, docID)
	if err != nil {
		return nil, fmt.Errorf("list document versions: %w", err)
	}
	out := make([]Version, len(rows))
	for i, r := range rows {
		out[i] = versionFromRow(r)
	}
	return out, nil
}
