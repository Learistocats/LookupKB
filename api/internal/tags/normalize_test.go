package tags

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNormalize covers T-6.2: arbitrary author input maps to a canonical
// lowercase-kebab slug across mixed case, whitespace, unicode and punctuation.
func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"My Tag":             "my-tag",
		"  spaced  out  ":    "spaced-out",
		"UPPER":              "upper",
		"already-kebab":      "already-kebab",
		"-leading-trailing-": "leading-trailing",
		"snake_case_tag":     "snake-case-tag",
		"dots.and.slashes/x": "dots-and-slashes-x",
		"multi   ---  seps":  "multi-seps",
		"Café Métrics":       "café-métrics", // unicode letters kept, lowercased
		"emoji 🎉 party":      "emoji-party",  // symbols become separators
		"C++":                "c",
		"123 numbers":        "123-numbers",
		"":                   "",
		"   ":                "",
		"!!!":                "",
	}
	for in, want := range cases {
		require.Equalf(t, want, Normalize(in), "Normalize(%q)", in)
	}
}

// TestNormalize_Idempotent asserts Normalize(Normalize(x)) == Normalize(x), the
// property that keeps the match-existing and create-new paths from diverging.
func TestNormalize_Idempotent(t *testing.T) {
	for _, in := range []string{"My Tag", "  weird__input!! ", "Café", "already-kebab", "a  b  c"} {
		once := Normalize(in)
		require.Equal(t, once, Normalize(once), "not idempotent for %q", in)
	}
}

// TestValidate_Normalizes proves valid input is normalized, deduped and returned.
func TestValidate_Normalizes(t *testing.T) {
	got, err := Validate(DefaultLimits, []string{"My Tag", "my-tag", "Second"})
	require.NoError(t, err)
	require.Equal(t, []string{"my-tag", "second"}, got, "case-variant duplicate collapses")
}

// TestValidate_CountBounds covers AC-6.3: fewer than min or more than max tags is
// rejected with a clear violation.
func TestValidate_CountBounds(t *testing.T) {
	limits := Limits{MinTags: 1, MaxTags: 3, MinLen: 2, MaxLen: 32}

	_, err := Validate(limits, nil)
	requireViolation(t, err, "at least 1")

	_, err = Validate(limits, []string{"a1", "b2", "c3", "d4"})
	requireViolation(t, err, "at most 3")

	// Exactly at the bounds is fine.
	_, err = Validate(limits, []string{"ok"})
	require.NoError(t, err)
	_, err = Validate(limits, []string{"aa", "bb", "cc"})
	require.NoError(t, err)
}

// TestValidate_LengthBounds rejects tags shorter/longer than the per-tag limits.
func TestValidate_LengthBounds(t *testing.T) {
	limits := Limits{MinTags: 1, MaxTags: 8, MinLen: 2, MaxLen: 5}

	_, err := Validate(limits, []string{"a"}) // 1 char < min 2
	requireViolation(t, err, "2–5 characters")

	_, err = Validate(limits, []string{"toolongtag"}) // > max 5
	requireViolation(t, err, "2–5 characters")

	_, err = Validate(limits, []string{"good"})
	require.NoError(t, err)
}

// TestValidate_DropsEmptyAsViolation ensures an all-punctuation "tag" is reported,
// not silently swallowed, and does not count toward the minimum.
func TestValidate_RejectsEmptyTag(t *testing.T) {
	_, err := Validate(DefaultLimits, []string{"!!!"})
	requireViolation(t, err, "not a valid tag")
}

// TestValidate_AggregatesViolations confirms every problem is collected (so the
// publish gate can show all per-field errors at once), not just the first.
func TestValidate_AggregatesViolations(t *testing.T) {
	limits := Limits{MinTags: 1, MaxTags: 8, MinLen: 3, MaxLen: 10}
	_, err := Validate(limits, []string{"a", "bb"})
	var verr *ValidationError
	require.True(t, errors.As(err, &verr))
	require.Len(t, verr.Violations, 2, "both under-length tags reported")
}

func requireViolation(t *testing.T, err error, substr string) {
	t.Helper()
	var verr *ValidationError
	require.True(t, errors.As(err, &verr), "expected *ValidationError, got %v", err)
	require.Condition(t, func() bool {
		for _, v := range verr.Violations {
			if strings.Contains(v, substr) {
				return true
			}
		}
		return false
	}, "no violation contained %q in %v", substr, verr.Violations)
}
