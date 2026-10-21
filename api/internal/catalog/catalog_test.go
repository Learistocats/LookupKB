package catalog

import (
	"context"
	"testing"
	"time"

	"github.com/Learistocats/LookupKB/api/internal/access"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	row     dbgen.GetPublishedBySlugRow
	rowErr  error
	denied  []string
	listArg dbgen.ListPublishedDocumentsParams
	rows    []dbgen.ListPublishedDocumentsRow
}

func (f *fakeStore) GetPublishedBySlug(context.Context, string) (dbgen.GetPublishedBySlugRow, error) {
	return f.row, f.rowErr
}

func (f *fakeStore) ListPublishedDocuments(_ context.Context, arg dbgen.ListPublishedDocumentsParams) ([]dbgen.ListPublishedDocumentsRow, error) {
	f.listArg = arg
	return f.rows, nil
}

func (f *fakeStore) ListDeniedProtectedTags(context.Context, pgtype.UUID) ([]dbgen.ListDeniedProtectedTagsRow, error) {
	out := []dbgen.ListDeniedProtectedTagsRow{}
	for _, s := range f.denied {
		out = append(out, dbgen.ListDeniedProtectedTagsRow{ID: pgtype.UUID{Bytes: uuid.New(), Valid: true}, Slug: s, Label: s})
	}
	return out, nil
}

func caller() access.Caller {
	return access.Caller{UserID: pgtype.UUID{Bytes: uuid.New(), Valid: true}}
}

func TestCursorRoundTrip(t *testing.T) {
	at := pgtype.Timestamptz{Time: time.Date(2026, 10, 20, 12, 0, 0, 123456000, time.UTC), Valid: true}
	id := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	gotAt, gotID, err := decodeCursor(encodeCursor(at, id))
	require.NoError(t, err)
	require.True(t, gotAt.Time.Equal(at.Time), "microsecond precision survives")
	require.Equal(t, id, gotID)

	for _, bad := range []string{"%%", "bm8", "MTIzLm5vdC1hLXV1aWQ", "eC4wMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDA"} {
		_, _, err := decodeCursor(bad)
		require.ErrorIs(t, err, ErrInvalidCursor, bad)
	}
}

func TestGet_DeniedIsNotFoundAndBodyFormats(t *testing.T) {
	html := "<p>Hi <b>there</b></p>"
	md := "Hi **there**"
	store := &fakeStore{row: dbgen.GetPublishedBySlugRow{
		Slug: "s", RenderedHtml: &html, PublishedMd: &md, TagSlugs: []string{"secret"},
		AuthorID: pgtype.UUID{Bytes: uuid.New(), Valid: true},
	}}
	svc := New(store)

	d, err := svc.Get(context.Background(), caller(), "s")
	require.NoError(t, err)
	for f, want := range map[Format]string{FormatHTML: html, FormatMarkdown: md, FormatText: "Hi there"} {
		got, err := d.Body(f)
		require.NoError(t, err)
		require.Equal(t, want, got, f)
	}
	require.Equal(t, []string{}, d.Tags)

	store.denied = []string{"secret"}
	_, err = svc.Get(context.Background(), caller(), "s")
	require.ErrorIs(t, err, ErrNotFound)

	store.rowErr = pgx.ErrNoRows
	_, err = svc.Get(context.Background(), caller(), "s")
	require.ErrorIs(t, err, ErrNotFound)

	// A zero caller fails closed (never anonymous).
	_, err = svc.Get(context.Background(), access.Caller{}, "s")
	require.Error(t, err)
}

func TestParseFormat(t *testing.T) {
	f, ok := ParseFormat(" Markdown ")
	require.True(t, ok)
	require.Equal(t, FormatMarkdown, f)
	_, ok = ParseFormat("pdf")
	require.False(t, ok)
}
