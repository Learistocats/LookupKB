package publish

import (
	"context"
	"testing"

	"github.com/Learistocats/LookupKB/api/internal/access"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

// policyQuerier returns a different denied set on each call, to model an admin
// protecting a tag between the pre-render check and the in-transaction re-check.
type policyQuerier struct {
	perCall [][]string
	calls   int
}

func (q *policyQuerier) ListDeniedProtectedTags(_ context.Context, _ pgtype.UUID) ([]dbgen.ListDeniedProtectedTagsRow, error) {
	set := q.perCall[min(q.calls, len(q.perCall)-1)]
	q.calls++
	rows := make([]dbgen.ListDeniedProtectedTagsRow, 0, len(set))
	for _, s := range set {
		rows = append(rows, dbgen.ListDeniedProtectedTagsRow{Slug: s, Label: s})
	}
	return rows, nil
}

func author() access.Caller {
	return access.Caller{UserID: pgtype.UUID{Bytes: uuid.New(), Valid: true}}
}

func TestProtectedTagErrors_DeniedTagIsPerFieldError(t *testing.T) {
	q := &policyQuerier{perCall: [][]string{{"reeflect"}}}
	errs, err := ProtectedTagErrors(context.Background(), q, author(), []string{"go", "reeflect"})
	require.NoError(t, err)
	require.Equal(t, []FieldError{{
		Field:   FieldTags,
		Message: `tag "reeflect" is protected; you don't have access to it`,
	}}, errs)
}

func TestProtectedTagErrors_GrantedOrPublicTagsPass(t *testing.T) {
	q := &policyQuerier{perCall: [][]string{{"hr"}}}
	errs, err := ProtectedTagErrors(context.Background(), q, author(), []string{"go", "reeflect"})
	require.NoError(t, err)
	require.Empty(t, errs)
}

func TestProtectedTagErrors_SuperAdminNeverRefused(t *testing.T) {
	q := &policyQuerier{perCall: [][]string{{"reeflect"}}}
	c := author()
	c.IsSuperAdmin = true
	errs, err := ProtectedTagErrors(context.Background(), q, c, []string{"reeflect"})
	require.NoError(t, err)
	require.Empty(t, errs)
	require.Zero(t, q.calls)
}

// The pre-render check passes, then the tag becomes protected before the
// in-transaction re-check: the second call must refuse it.
func TestProtectedTagErrors_RecheckSeesPolicyChange(t *testing.T) {
	q := &policyQuerier{perCall: [][]string{{}, {"reeflect"}}}
	c := author()
	pre, err := ProtectedTagErrors(context.Background(), q, c, []string{"reeflect"})
	require.NoError(t, err)
	require.Empty(t, pre)
	inTx, err := ProtectedTagErrors(context.Background(), q, c, []string{"reeflect"})
	require.NoError(t, err)
	require.Len(t, inTx, 1)
	require.Equal(t, FieldTags, inTx[0].Field)
}

func TestNormalizedSet(t *testing.T) {
	require.Equal(t, []string{"reeflect", "my-tag"}, normalizedSet([]string{" Reeflect ", "My Tag", "!!"}))
}
