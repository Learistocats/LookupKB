package access

import (
	"context"
	"errors"
	"testing"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

type fakeQuerier struct {
	calls int
	rows  []dbgen.ListDeniedProtectedTagsRow
	err   error
	arg   pgtype.UUID
}

func (f *fakeQuerier) ListDeniedProtectedTags(_ context.Context, userID pgtype.UUID) ([]dbgen.ListDeniedProtectedTagsRow, error) {
	f.calls++
	f.arg = userID
	return f.rows, f.err
}

func id() pgtype.UUID { return pgtype.UUID{Bytes: uuid.New(), Valid: true} }

func denied(slugs ...string) []dbgen.ListDeniedProtectedTagsRow {
	out := make([]dbgen.ListDeniedProtectedTagsRow, 0, len(slugs))
	for _, s := range slugs {
		out = append(out, dbgen.ListDeniedProtectedTagsRow{ID: id(), Slug: s, Label: s})
	}
	return out
}

func TestLoad_SuperAdminSkipsQueryAndReadsEverything(t *testing.T) {
	q := &fakeQuerier{rows: denied("secret")}
	p, err := Load(context.Background(), q, Caller{UserID: id(), IsSuperAdmin: true})
	require.NoError(t, err)
	require.Zero(t, q.calls, "super-admin needs no policy query")
	require.True(t, p.Unrestricted())
	require.True(t, p.CanRead(id(), []string{"secret"}))
	require.Empty(t, p.DeniedAmong([]string{"secret"}))
	require.Empty(t, p.DeniedIDs())
}

func TestLoad_UsesCallerAndPropagatesErrors(t *testing.T) {
	caller := id()
	q := &fakeQuerier{rows: denied("secret")}
	p, err := Load(context.Background(), q, Caller{UserID: caller})
	require.NoError(t, err)
	require.Equal(t, caller, q.arg)
	require.False(t, p.Unrestricted())
	require.Len(t, p.DeniedIDs(), 1)

	_, err = Load(context.Background(), &fakeQuerier{err: errors.New("db down")}, Caller{UserID: caller})
	require.Error(t, err)

	_, err = Load(context.Background(), q, Caller{})
	require.Error(t, err, "a caller without a user id fails closed")
}

func TestCanRead_Matrix(t *testing.T) {
	caller, author := id(), id()
	p := NewPolicy(Caller{UserID: caller}, []Tag{{ID: id(), Slug: "reeflect"}, {ID: id(), Slug: "hr"}})

	cases := []struct {
		name   string
		author pgtype.UUID
		tags   []string
		want   bool
	}{
		{"untagged", author, nil, true},
		{"only public tags", author, []string{"go", "ops"}, true},
		{"one denied protected tag", author, []string{"go", "reeflect"}, false},
		{"all of several protected must be granted", author, []string{"hr", "reeflect"}, false},
		{"author reads own entry despite denied tag", caller, []string{"reeflect"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, p.CanRead(tc.author, tc.tags))
		})
	}
}

// The ALL rule: a member of a group granting only one of two protected tags on an
// entry is still denied — the denied set holds the tag they lack.
func TestCanRead_PartialGrantIsDenied(t *testing.T) {
	// Granted "reeflect" (so it is not in the denied set) but not "hr".
	p := NewPolicy(Caller{UserID: id()}, []Tag{{ID: id(), Slug: "hr"}})
	require.True(t, p.CanRead(id(), []string{"reeflect"}))
	require.False(t, p.CanRead(id(), []string{"reeflect", "hr"}))
}

func TestZeroPolicyFailsClosed(t *testing.T) {
	var p Policy
	require.False(t, p.Loaded())
	require.False(t, p.Unrestricted())
	require.False(t, p.CanRead(id(), nil))
}

func TestDeniedAmong_OrderAndDedupe(t *testing.T) {
	p := NewPolicy(Caller{UserID: id()}, []Tag{{ID: id(), Slug: "b"}, {ID: id(), Slug: "a"}})
	require.Equal(t, []string{"a", "b"}, p.DeniedAmong([]string{"x", "a", "b", "a"}))
	require.Empty(t, p.DeniedAmong([]string{"x"}))
}
