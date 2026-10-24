package search

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Learistocats/LookupKB/api/internal/access"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func caller() (access.Caller, string) {
	u := uuid.New()
	return access.Caller{UserID: pgtype.UUID{Bytes: u, Valid: true}}, u.String()
}

type fakeDenied []access.Tag

func (d fakeDenied) ListDeniedProtectedTags(context.Context, pgtype.UUID) ([]dbgen.ListDeniedProtectedTagsRow, error) {
	rows := make([]dbgen.ListDeniedProtectedTagsRow, len(d))
	for i, t := range d {
		rows[i] = dbgen.ListDeniedProtectedTagsRow{ID: t.ID, Slug: t.Slug, Label: t.Label}
	}
	return rows, nil
}

// policy resolves a caller's policy through access.Load with a fixed denied set.
func policy(c access.Caller, denied []access.Tag) access.Policy {
	p, err := access.Load(context.Background(), fakeDenied(denied), c)
	if err != nil {
		panic(err)
	}
	return p
}

func tag(slug, label string) access.Tag {
	return access.Tag{ID: pgtype.UUID{Bytes: uuid.New(), Valid: true}, Slug: slug, Label: label}
}

func TestAccessFilter_DeniedTags(t *testing.T) {
	c, me := caller()
	f, err := accessFilter(policy(c, []access.Tag{tag("hr", "hr"), tag("reeflect", "reeflect")}))
	require.NoError(t, err)
	require.Equal(t, `author_id = "`+me+`" OR tags NOT IN ["hr", "reeflect"]`, f)
}

func TestAccessFilter_LabelAndSlugBothDenied(t *testing.T) {
	c, me := caller()
	f, err := accessFilter(policy(c, []access.Tag{tag("reeflect", "Reeflect")}))
	require.NoError(t, err)
	require.Equal(t, `author_id = "`+me+`" OR tags NOT IN ["Reeflect", "reeflect"]`, f)
}

func TestAccessFilter_UnrestrictedHasNoFilter(t *testing.T) {
	c, _ := caller()
	f, err := accessFilter(policy(c, nil))
	require.NoError(t, err)
	require.Empty(t, f, "nothing denied: every published entry is visible")

	c.IsSuperAdmin = true
	f, err = accessFilter(policy(c, []access.Tag{tag("hr", "hr")}))
	require.NoError(t, err)
	require.Empty(t, f, "super-admin sees everything")
}

func TestAccessFilter_ZeroPolicyFailsClosed(t *testing.T) {
	_, err := accessFilter(access.Policy{})
	require.Error(t, err)
}

// A denied label with filter metacharacters is escaped, never spliced raw: the
// quote cannot terminate the string and open a new clause.
func TestAccessFilter_EscapesQuotes(t *testing.T) {
	c, me := caller()
	f, err := accessFilter(policy(c, []access.Tag{tag("x", `x" OR author_id EXISTS OR tags = "y`)}))
	require.NoError(t, err)
	require.Equal(t, `author_id = "`+me+`" OR tags NOT IN ["x\" OR author_id EXISTS OR tags = \"y", "x"]`, f)
}

// A denied label that cannot be represented exactly fails the query closed — it
// must never be dropped from the deny list (that would widen access).
func TestAccessFilter_UnrepresentableLabelFailsClosed(t *testing.T) {
	c, _ := caller()
	for _, bad := range []string{`back\slash`, `trail\`, "ctl\x01", "nl\n"} {
		_, err := accessFilter(policy(c, []access.Tag{tag("ok", bad)}))
		require.Error(t, err, bad)
	}
}

func TestQuoteFilterValue(t *testing.T) {
	q, err := quoteFilterValue(`café "quoted" ]`)
	require.NoError(t, err)
	require.Equal(t, `"café \"quoted\" ]"`, q)
	_, err = quoteFilterValue(`a\"`)
	require.Error(t, err)
}

func TestBuildFilter_ClientFiltersAreSeparateANDedElements(t *testing.T) {
	c, me := caller()
	author := uuid.NewString()
	p := policy(c, []access.Tag{tag("hr", "hr")})
	got, err := buildFilter(p, Filters{
		Tags:          []string{"go", `x") OR (author_id EXISTS`},
		AuthorID:      author,
		PublishedFrom: "2026-01-01",
		PublishedTo:   "2026-01-31",
	})
	require.NoError(t, err)
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	to := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC).Unix() - 1
	require.Equal(t, []string{
		`author_id = "` + me + `" OR tags NOT IN ["hr"]`,
		`tags = "go"`,
		`tags = "x\") OR (author_id EXISTS"`,
		`author_id = "` + author + `"`,
		"published_at >= " + itoa(from),
		"published_at <= " + itoa(to),
	}, got, "the access filter is always the first element and client filters only add AND-ed elements")
}

// No client filter can remove or replace the access filter: whatever the client
// sends, the access expression is present verbatim as its own array element.
func TestBuildFilter_ClientCannotWidenAccess(t *testing.T) {
	c, _ := caller()
	p := policy(c, []access.Tag{tag("hr", "hr")})
	want, err := accessFilter(p)
	require.NoError(t, err)

	attempts := []Filters{
		{},
		{Tags: []string{`hr"] OR tags IN ["hr`}},
		{AuthorID: uuid.NewString()},
		{PublishedFrom: "2020-01-01T00:00:00Z"},
	}
	for _, f := range attempts {
		got, err := buildFilter(p, f)
		require.NoError(t, err)
		require.NotEmpty(t, got)
		require.Equal(t, want, got[0])
		for _, e := range got[1:] {
			require.False(t, strings.Contains(e, "NOT IN"), "client expressions are positive narrowing filters only")
		}
	}
}

func TestBuildFilter_RejectsInvalidClientFilters(t *testing.T) {
	c, _ := caller()
	p := policy(c, nil)
	bad := []Filters{
		{AuthorID: `x" OR author_id EXISTS`},
		{PublishedFrom: "yesterday"},
		{PublishedTo: "1 OR 1"},
		{Tags: []string{`a\`}},
		{Tags: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}},
	}
	for _, f := range bad {
		_, err := buildFilter(p, f)
		require.True(t, errors.Is(err, ErrInvalidFilter), "%+v", f)
	}
}

func TestAllowedFacets(t *testing.T) {
	got, err := allowedFacets([]string{"tags", " tags ", ""})
	require.NoError(t, err)
	require.Equal(t, []string{"tags"}, got)

	for _, f := range []string{"author_id", "body_text", "access"} {
		_, err := allowedFacets([]string{f})
		require.ErrorIs(t, err, ErrInvalidFilter, f)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
