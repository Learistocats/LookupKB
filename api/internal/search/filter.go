package search

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Learistocats/LookupKB/api/internal/access"
	"github.com/google/uuid"
)

// ErrInvalidFilter marks a client filter or facet the API refuses (400). Client
// filters are an allowlist; anything outside it is an error, never ignored into a
// wider query.
var ErrInvalidFilter = errors.New("invalid search filter")

// Client filter bounds.
const (
	maxFilterTags = 8
	// FacetTags is the only facet the API computes.
	FacetTags = "tags"
)

// AccessFilter builds the Meilisearch filter every query of this caller carries
// (LOO-33). An entry is visible when the caller authored it, or when it carries
// none of the protected tags the caller is denied:
//
//	author_id = "<uid>" OR tags NOT IN ["<denied>", …]
//
// It returns "" (no filter) when the caller is unrestricted — a super-admin, or
// nobody-denied — so every published entry is visible. The denied set comes from
// the database per request, so a policy change needs no reindex. A zero
// (unresolved) policy is refused rather than read as unrestricted.
//
// The index stores tag labels; the denied list carries both each tag's label and
// its slug (identical today: labels are written as the normalized slug) so a
// label drift can never let a denied tag through. Every value is quoted with
// quoteFilterValue; the caller id is validated as a UUID.
func AccessFilter(p access.Policy) (string, error) {
	if !p.Loaded() {
		return "", errors.New("search: access policy not resolved")
	}
	if p.Unrestricted() {
		return "", nil
	}
	uid := p.Caller().UserID
	if !uid.Valid {
		return "", errors.New("search: caller has no user id")
	}
	seen := map[string]struct{}{}
	values := make([]string, 0, 2*len(p.Denied()))
	for _, t := range p.Denied() {
		for _, v := range []string{t.Label, t.Slug} {
			if _, dup := seen[v]; dup || v == "" {
				continue
			}
			seen[v] = struct{}{}
			q, err := quoteFilterValue(v)
			if err != nil {
				// Fail closed: a denied tag we cannot express would widen access.
				return "", fmt.Errorf("search: denied tag %q: %w", v, err)
			}
			values = append(values, q)
		}
	}
	return `author_id = ` + strconv.Quote(uuid.UUID(uid.Bytes).String()) +
		` OR tags NOT IN [` + strings.Join(values, ", ") + `]`, nil
}

// Filters are the client-requested narrowing filters (LOO-34). They are an
// allowlist — tags, author, published_at range — each validated and quoted, and
// sent to Meilisearch as separate elements of the filter array, which Meilisearch
// ANDs together with the access filter. A client filter can therefore only ever
// narrow the access-filtered result set, never widen it.
type Filters struct {
	// Tags: every listed tag must be on the entry (drill-down, AND).
	Tags []string
	// AuthorID restricts to one author (users.id).
	AuthorID string
	// PublishedFrom/PublishedTo bound published_at (inclusive): RFC 3339 or a
	// YYYY-MM-DD date (To-date covers the whole day, UTC).
	PublishedFrom string
	PublishedTo   string
}

// expressions validates the filters and renders one Meilisearch expression per
// active filter.
func (f Filters) expressions() ([]string, error) {
	var out []string
	if len(f.Tags) > maxFilterTags {
		return nil, fmt.Errorf("%w: at most %d tags", ErrInvalidFilter, maxFilterTags)
	}
	for _, t := range f.Tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		q, err := quoteFilterValue(t)
		if err != nil {
			return nil, fmt.Errorf("%w: tag: %v", ErrInvalidFilter, err)
		}
		out = append(out, "tags = "+q)
	}
	if f.AuthorID != "" {
		id, err := uuid.Parse(f.AuthorID)
		if err != nil {
			return nil, fmt.Errorf("%w: author_id", ErrInvalidFilter)
		}
		out = append(out, "author_id = "+strconv.Quote(id.String()))
	}
	if f.PublishedFrom != "" {
		ts, err := parseFilterTime(f.PublishedFrom, false)
		if err != nil {
			return nil, fmt.Errorf("%w: published_from", ErrInvalidFilter)
		}
		out = append(out, "published_at >= "+strconv.FormatInt(ts, 10))
	}
	if f.PublishedTo != "" {
		ts, err := parseFilterTime(f.PublishedTo, true)
		if err != nil {
			return nil, fmt.Errorf("%w: published_to", ErrInvalidFilter)
		}
		out = append(out, "published_at <= "+strconv.FormatInt(ts, 10))
	}
	return out, nil
}

// parseFilterTime reads RFC 3339 or YYYY-MM-DD into unix seconds. A date-only
// upper bound means the end of that day (UTC).
func parseFilterTime(s string, endOfDay bool) (int64, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Unix(), nil
	}
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return 0, err
	}
	if endOfDay {
		return d.Add(24*time.Hour).Unix() - 1, nil
	}
	return d.Unix(), nil
}

// buildFilter composes the access filter and the client filters into the
// Meilisearch filter array (elements are ANDed). nil means no filter at all.
func buildFilter(p access.Policy, f Filters) ([]string, error) {
	accessExpr, err := AccessFilter(p)
	if err != nil {
		return nil, err
	}
	client, err := f.expressions()
	if err != nil {
		return nil, err
	}
	var out []string
	if accessExpr != "" {
		out = append(out, accessExpr)
	}
	return append(out, client...), nil
}

// quoteFilterValue renders s as a double-quoted Meilisearch filter string.
//
// Meilisearch's filter parser only unescapes \" inside a quoted value — a
// backslash otherwise stays literal, and one before the closing quote would
// escape it. So a value is accepted only when it can be represented exactly:
// quotes are escaped as \", while backslashes and control characters are refused
// (tag slugs/labels never contain them; refusing keeps the encoding unambiguous).
func quoteFilterValue(s string) (string, error) {
	for _, r := range s {
		if r == '\\' {
			return "", errors.New("backslash is not allowed")
		}
		if r < 0x20 || r == 0x7f {
			return "", errors.New("control character is not allowed")
		}
	}
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`, nil
}
