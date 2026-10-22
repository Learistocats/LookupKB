package publish

import (
	"strings"
	"testing"

	"github.com/Learistocats/LookupKB/api/internal/tags"
	"github.com/stretchr/testify/require"
)

// fieldsWithError returns the set of field names that have at least one error.
func fieldsWithError(errs []FieldError) map[string]bool {
	out := map[string]bool{}
	for _, e := range errs {
		out[e.Field] = true
	}
	return out
}

var (
	defML = DefaultMetadataLimits
	defTL = tags.DefaultLimits
)

// fully valid metadata produces no errors and the normalized tag set.
func TestValidateMetadata_Valid(t *testing.T) {
	norm, errs := ValidateMetadata(defML, defTL, MetadataInput{
		Title:       "How to deploy LookupKB",
		Description: "A short but sufficient description of the deployment steps.",
		Tags:        []string{"Ops", "deploy"},
	})
	require.Empty(t, errs)
	require.Equal(t, []string{"ops", "deploy"}, norm)
}

// a missing title, too-short description and zero tags each surface as a
// per-field error on their own field — all at once, not fail-fast.
func TestValidateMetadata_PerFieldViolations(t *testing.T) {
	_, errs := ValidateMetadata(defML, defTL, MetadataInput{
		Title:       "",
		Description: "short",
		Tags:        nil,
	})
	fields := fieldsWithError(errs)
	require.True(t, fields[FieldTitle], "title error expected")
	require.True(t, fields[FieldDescription], "description error expected")
	require.True(t, fields[FieldTags], "tags error expected")
}

// Whitespace-only values do not satisfy a minimum (trimmed before measuring).
func TestValidateMetadata_WhitespaceIsEmpty(t *testing.T) {
	_, errs := ValidateMetadata(defML, defTL, MetadataInput{
		Title:       "   ",
		Description: "                                   ",
		Tags:        []string{"ok"},
	})
	fields := fieldsWithError(errs)
	require.True(t, fields[FieldTitle])
	require.True(t, fields[FieldDescription])
}

// A title over the max length is rejected on the title field.
func TestValidateMetadata_TitleTooLong(t *testing.T) {
	_, errs := ValidateMetadata(defML, defTL, MetadataInput{
		Title:       strings.Repeat("a", defML.Title.Max+1),
		Description: "A perfectly adequate description here.",
		Tags:        []string{"ok"},
	})
	require.True(t, fieldsWithError(errs)[FieldTitle])
}

// Titles must be single-line: an embedded newline is a title violation.
func TestValidateMetadata_MultilineTitle(t *testing.T) {
	_, errs := ValidateMetadata(defML, defTL, MetadataInput{
		Title:       "line one\nline two",
		Description: "A perfectly adequate description here.",
		Tags:        []string{"ok"},
	})
	require.True(t, fieldsWithError(errs)[FieldTitle])
}

// more than the max number of tags is blocked on the tags field.
func TestValidateMetadata_TooManyTags(t *testing.T) {
	many := make([]string, defTL.MaxTags+1)
	for i := range many {
		many[i] = "tag" + string(rune('a'+i))
	}
	_, errs := ValidateMetadata(defML, defTL, MetadataInput{
		Title:       "A good enough title",
		Description: "A perfectly adequate description here.",
		Tags:        many,
	})
	require.True(t, fieldsWithError(errs)[FieldTags])
}

// A tag shorter than the per-tag minimum is a tags-field violation.
func TestValidateMetadata_ShortTag(t *testing.T) {
	_, errs := ValidateMetadata(defML, defTL, MetadataInput{
		Title:       "A good enough title",
		Description: "A perfectly adequate description here.",
		Tags:        []string{"a"},
	})
	require.True(t, fieldsWithError(errs)[FieldTags])
}
