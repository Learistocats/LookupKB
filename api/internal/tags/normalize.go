// Package tags owns LookupKB's tag vocabulary: the single normalization rule,
// limit validation, the autocomplete query, and live usage_count maintenance.
// Tags are the only organizing structure in the KB, so normalization must be
// deterministic and applied identically everywhere or near-duplicates ("api" vs
// "-api-") fragment the search facets.
//
// This file is the pure logic (no database): Normalize and Validate.
package tags

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Normalize maps arbitrary author input to a canonical tag slug: trimmed,
// lower-cased, with every run of characters that are not Unicode letters or
// digits collapsed to a single hyphen, and leading/trailing hyphens removed.
//
// It is idempotent — Normalize(Normalize(x)) == Normalize(x) — and must be the
// single normalization applied on BOTH the autocomplete-match and create-new
// paths (ticket "things not to miss"). Unicode letters are kept (lower-cased), so
// "Café Métrics" → "café-métrics"; ASCII punctuation/whitespace becomes hyphens.
func Normalize(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))

	// pendingHyphen defers writing a separator until a real letter/digit follows,
	// which collapses runs and drops leading/trailing separators in one pass.
	pendingHyphen := false
	wrote := false
	for _, r := range strings.TrimSpace(raw) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if pendingHyphen && wrote {
				b.WriteByte('-')
			}
			pendingHyphen = false
			b.WriteRune(unicode.ToLower(r))
			wrote = true
			continue
		}
		// Any other rune (space, punctuation, symbol) is a separator.
		pendingHyphen = true
	}
	return b.String()
}

// Limits are the admin-tunable tag bounds. Lengths are
// measured in runes, not bytes, so multi-byte tags are not unfairly rejected.
type Limits struct {
	MinTags int `json:"min_tags"`
	MaxTags int `json:"max_tags"`
	MinLen  int `json:"min_len"`
	MaxLen  int `json:"max_len"`
}

// DefaultLimits allow 1–8 tags per document, each 2–32 characters. LoadLimits
// overrides these from the settings table.
var DefaultLimits = Limits{MinTags: 1, MaxTags: 8, MinLen: 2, MaxLen: 32}

// ValidationError aggregates every tag limit violation so the author sees all of
// them at once. Validate returns it when the normalized set is invalid.
type ValidationError struct {
	Violations []string
}

func (e *ValidationError) Error() string {
	return "invalid tags: " + strings.Join(e.Violations, "; ")
}

// Validate normalizes and de-duplicates raw tag input, then checks it against the
// limits. It returns the canonical slug set (order-preserving, deduped) and, when
// the set violates the limits, a *ValidationError listing every problem.
//
// Never trust a client's own count/length checks. Blank tags normalize to "" and
// are dropped before counting.
func Validate(limits Limits, raw []string) ([]string, error) {
	seen := make(map[string]struct{}, len(raw))
	normalized := make([]string, 0, len(raw))
	var violations []string

	for _, r := range raw {
		slug := Normalize(r)
		if slug == "" {
			// A tag that normalizes to nothing (empty or all-punctuation) is not a
			// real tag; report it so the author can fix the input, don't silently drop.
			violations = append(violations, fmt.Sprintf("%q is not a valid tag", r))
			continue
		}
		if _, dup := seen[slug]; dup {
			continue // de-dupe: "My Tag" and "my-tag" collapse to one.
		}
		seen[slug] = struct{}{}
		normalized = append(normalized, slug)

		if n := utf8.RuneCountInString(slug); n < limits.MinLen || n > limits.MaxLen {
			violations = append(violations,
				fmt.Sprintf("tag %q must be %d–%d characters", slug, limits.MinLen, limits.MaxLen))
		}
	}

	if len(normalized) < limits.MinTags {
		violations = append(violations,
			fmt.Sprintf("at least %d tag(s) required", limits.MinTags))
	}
	if len(normalized) > limits.MaxTags {
		violations = append(violations,
			fmt.Sprintf("at most %d tags allowed (got %d)", limits.MaxTags, len(normalized)))
	}

	if len(violations) > 0 {
		return normalized, &ValidationError{Violations: violations}
	}
	return normalized, nil
}
