// Package access is the single home of LookupKB's read-access rule for published
// entries: protected tags.
//
//   - A tag is protected iff at least one group grants it (group_tag_grants).
//   - A caller is granted a protected tag iff they belong to a granting group.
//   - A published entry is readable iff the caller is a super-admin, OR its
//     author, OR granted EVERY protected tag the entry carries.
//
// Drafts are not covered here: they stay author-only in their owner-scoped SQL.
//
// The read path, the publish gate, tag autocomplete and search all load a Policy
// with Load and ask it — none of them re-implements the rule. The denied set is
// read from the database on every request, so a policy or membership change takes
// effect on the caller's next request without a reindex.
package access

import (
	"context"
	"fmt"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/jackc/pgx/v5/pgtype"
)

// Caller is the verified identity an access decision is made for. Both fields
// come from the auth middleware (users.is_super_admin is re-read per request).
type Caller struct {
	UserID       pgtype.UUID
	IsSuperAdmin bool
}

// Querier loads the protected tags a user is not granted. *dbgen.Queries (pool- or
// tx-bound) satisfies it.
type Querier interface {
	ListDeniedProtectedTags(ctx context.Context, userID pgtype.UUID) ([]dbgen.ListDeniedProtectedTagsRow, error)
}

// Tag is a protected tag the caller is denied.
type Tag struct {
	ID    pgtype.UUID
	Slug  string
	Label string
}

// Policy is one caller's resolved access policy, built with Load.
// The zero value is fail-closed: it reads nothing and is never Unrestricted, so a
// forgotten Load can never widen access.
type Policy struct {
	loaded bool
	caller Caller
	denied []Tag
	slugs  map[string]struct{}
}

// Load resolves the caller's policy. A super-admin is unrestricted and costs no
// query; anyone else gets their denied protected tags from q. A caller without a
// valid user id is refused (fail closed) rather than treated as anonymous.
func Load(ctx context.Context, q Querier, c Caller) (Policy, error) {
	if c.IsSuperAdmin {
		return Policy{loaded: true, caller: c}, nil
	}
	if !c.UserID.Valid {
		return Policy{}, fmt.Errorf("access: caller has no user id")
	}
	rows, err := q.ListDeniedProtectedTags(ctx, c.UserID)
	if err != nil {
		return Policy{}, fmt.Errorf("access: load denied protected tags: %w", err)
	}
	denied := make([]Tag, 0, len(rows))
	for _, r := range rows {
		denied = append(denied, Tag{ID: r.ID, Slug: r.Slug, Label: r.Label})
	}
	return newPolicy(c, denied), nil
}

func newPolicy(c Caller, denied []Tag) Policy {
	slugs := make(map[string]struct{}, len(denied))
	for _, t := range denied {
		slugs[t.Slug] = struct{}{}
	}
	return Policy{loaded: true, caller: c, denied: denied, slugs: slugs}
}

// Caller returns the identity the policy was resolved for.
func (p Policy) Caller() Caller { return p.caller }

// Unrestricted reports whether the caller may read every published entry: a
// super-admin, or a caller with no denied protected tag at all.
func (p Policy) Unrestricted() bool {
	return p.loaded && (p.caller.IsSuperAdmin || len(p.denied) == 0)
}

// Loaded reports whether the policy was resolved (false for the zero value).
func (p Policy) Loaded() bool { return p.loaded }

// Denied returns the protected tags the caller lacks (nil for a super-admin).
func (p Policy) Denied() []Tag { return p.denied }

// DeniedIDs returns the ids of Denied, for SQL exclusion (never nil, so it binds
// as an empty uuid[]).
func (p Policy) DeniedIDs() []pgtype.UUID {
	ids := make([]pgtype.UUID, 0, len(p.denied))
	for _, t := range p.denied {
		ids = append(ids, t.ID)
	}
	return ids
}

// IsDenied reports whether the tag slug is a protected tag the caller lacks.
func (p Policy) IsDenied(slug string) bool {
	_, ok := p.slugs[slug]
	return ok
}

// DeniedAmong returns, in input order and de-duplicated, the slugs that are
// protected tags the caller lacks. The publish gate uses it to refuse tagging an
// entry with a tag the author has no access to.
func (p Policy) DeniedAmong(slugs []string) []string {
	var out []string
	seen := make(map[string]struct{}, len(slugs))
	for _, s := range slugs {
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		if p.IsDenied(s) {
			out = append(out, s)
		}
	}
	return out
}

// CanRead decides read access to a PUBLISHED entry from its author and its tag
// slugs: super-admin, author, or no carried tag is denied (= every protected tag
// on it is granted — the ALL rule, not ANY).
func (p Policy) CanRead(authorID pgtype.UUID, tagSlugs []string) bool {
	if !p.loaded {
		return false
	}
	if p.caller.IsSuperAdmin {
		return true
	}
	if p.caller.UserID.Valid && p.caller.UserID == authorID {
		return true
	}
	for _, s := range tagSlugs {
		if p.IsDenied(s) {
			return false
		}
	}
	return true
}
