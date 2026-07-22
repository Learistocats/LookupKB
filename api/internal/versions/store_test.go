package versions

import (
	"context"
	"errors"
	"testing"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func pgUUID(t *testing.T) pgtype.UUID {
	t.Helper()
	return pgtype.UUID{Bytes: uuid.New(), Valid: true}
}

func strptr(s string) *string { return &s }

// fakeVersionWriter records the calls CreateSnapshot makes and returns canned rows /
// errors, so the snapshot orchestration is verified without a database. It also
// records the call order so we can assert the lock happens before the insert.
type fakeVersionWriter struct {
	lockID    pgtype.UUID
	lockErr   error
	createArg dbgen.CreateDocumentVersionParams
	createRow dbgen.DocumentVersion
	createErr error
	setArg    dbgen.SetCurrentVersionParams
	setErr    error

	calls []string
}

func (f *fakeVersionWriter) LockDocumentForPublish(_ context.Context, id pgtype.UUID) (pgtype.UUID, error) {
	f.calls = append(f.calls, "lock")
	f.lockID = id
	if f.lockErr != nil {
		return pgtype.UUID{}, f.lockErr
	}
	return id, nil
}

func (f *fakeVersionWriter) CreateDocumentVersion(_ context.Context, arg dbgen.CreateDocumentVersionParams) (dbgen.DocumentVersion, error) {
	f.calls = append(f.calls, "create")
	f.createArg = arg
	if f.createErr != nil {
		return dbgen.DocumentVersion{}, f.createErr
	}
	return f.createRow, nil
}

func (f *fakeVersionWriter) SetCurrentVersion(_ context.Context, arg dbgen.SetCurrentVersionParams) error {
	f.calls = append(f.calls, "set")
	f.setArg = arg
	return f.setErr
}

// TestCreateSnapshot_HappyPath proves CreateSnapshot locks the document, forwards
// every Snapshot field verbatim into the version insert, and repoints
// current_version_id at the *returned* snapshot id — in that order.
func TestCreateSnapshot_HappyPath(t *testing.T) {
	docID := pgUUID(t)
	verID := pgUUID(t)
	publisher := pgUUID(t)
	acl := []pgtype.UUID{pgUUID(t), pgUUID(t)}

	f := &fakeVersionWriter{
		createRow: dbgen.DocumentVersion{
			ID:           verID,
			DocumentID:   docID,
			VersionNo:    3,
			Title:        strptr("A title"),
			Description:  strptr("A description"),
			SourceMd:     strptr("# hi"),
			RenderedHtml: strptr("<h1>hi</h1>"),
			Tags:         []string{"go", "db"},
			Access:       dbgen.DocumentAccessRestricted,
			AclGroupIds:  acl,
			PublishedBy:  publisher,
		},
	}

	snap := Snapshot{
		DocumentID:   docID,
		Title:        strptr("A title"),
		Description:  strptr("A description"),
		SourceMD:     strptr("# hi"),
		RenderedHTML: strptr("<h1>hi</h1>"),
		Tags:         []string{"go", "db"},
		Access:       dbgen.DocumentAccessRestricted,
		ACLGroupIDs:  acl,
		PublishedBy:  publisher,
	}

	got, err := CreateSnapshot(context.Background(), f, snap)
	require.NoError(t, err)

	// Lock must precede the insert, which must precede the repoint.
	require.Equal(t, []string{"lock", "create", "set"}, f.calls)
	require.Equal(t, docID, f.lockID)

	// Every Snapshot field is copied verbatim into the insert params.
	require.Equal(t, dbgen.CreateDocumentVersionParams{
		DocumentID:   docID,
		Title:        snap.Title,
		Description:  snap.Description,
		SourceMd:     snap.SourceMD,
		RenderedHtml: snap.RenderedHTML,
		Tags:         snap.Tags,
		Access:       snap.Access,
		AclGroupIds:  snap.ACLGroupIDs,
		PublishedBy:  snap.PublishedBy,
	}, f.createArg)

	// current_version_id is repointed at the id the insert returned.
	require.Equal(t, dbgen.SetCurrentVersionParams{ID: docID, CurrentVersionID: verID}, f.setArg)

	// The returned Version mirrors the stored row.
	require.Equal(t, verID, got.ID)
	require.Equal(t, int32(3), got.VersionNo)
	require.Equal(t, dbgen.DocumentAccessRestricted, got.Access)
	require.Equal(t, acl, got.ACLGroupIDs)
}

// TestCreateSnapshot_LockMissAborts proves a missing/soft-deleted document (lock
// returns ErrNoRows) aborts before any write — no version row, no repoint.
func TestCreateSnapshot_LockMissAborts(t *testing.T) {
	f := &fakeVersionWriter{lockErr: pgx.ErrNoRows}

	got, err := CreateSnapshot(context.Background(), f, Snapshot{DocumentID: pgUUID(t)})
	require.Error(t, err)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.Nil(t, got)
	require.Equal(t, []string{"lock"}, f.calls, "must not insert or repoint after a lock miss")
}

// TestCreateSnapshot_InsertErrorAborts proves an insert failure aborts before the
// repoint, so current_version_id can't point at a snapshot that doesn't exist.
func TestCreateSnapshot_InsertErrorAborts(t *testing.T) {
	f := &fakeVersionWriter{createErr: errors.New("boom")}

	got, err := CreateSnapshot(context.Background(), f, Snapshot{DocumentID: pgUUID(t)})
	require.Error(t, err)
	require.Nil(t, got)
	require.Equal(t, []string{"lock", "create"}, f.calls, "must not repoint after an insert failure")
}

// TestCreateSnapshot_RepointErrorPropagates proves a repoint failure surfaces as an
// error (the caller's transaction then rolls the whole publish back).
func TestCreateSnapshot_RepointErrorPropagates(t *testing.T) {
	f := &fakeVersionWriter{setErr: errors.New("nope")}

	got, err := CreateSnapshot(context.Background(), f, Snapshot{DocumentID: pgUUID(t)})
	require.Error(t, err)
	require.Nil(t, got)
	require.Equal(t, []string{"lock", "create", "set"}, f.calls)
}
