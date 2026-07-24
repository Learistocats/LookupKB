package publish

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Learistocats/LookupKB/api/internal/tags"
)

// Field names used in FieldError.Field. The frontend groups errors by these to
// render them inline next to the offending metadata input (AC-5.2).
const (
	FieldTitle       = "title"
	FieldDescription = "description"
	FieldTags        = "tags"
	FieldContent     = "content"
)

// FieldError is one per-field validation failure. The publish handler returns a
// list of these as 422 so the client can show each message beside its field,
// rather than a single opaque blob error (ticket scope: "per-field error
// messages").
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// MetadataInput is the raw, client-supplied metadata a publish attempt carries.
// Title/description are trimmed and length-checked here; tags are normalized and
// counted by tags.Validate. source_md is not part of this struct — it is the
// server-held working copy, validated for size by the renderer, not here.
type MetadataInput struct {
	Title       string
	Description string
	Tags        []string
}

// ValidateMetadata checks a publish attempt's metadata against the current limits
// and returns the normalized (lowercase-kebab, deduped) tag set plus every
// per-field violation. It is a pure function (no DB, no HTTP) so the gate is
// unit-testable in isolation (ticket suggestion; T-5.2/T-6.3), and it is the
// authoritative check — client-side hints are never trusted (06-security.md §6).
//
// All fields are checked (not fail-fast) so the author sees every problem at once.
// Title/description are validated on their trimmed value, so leading/trailing
// whitespace can neither satisfy a minimum nor smuggle past a maximum.
func ValidateMetadata(ml MetadataLimits, tl tags.Limits, in MetadataInput) (normTags []string, errs []FieldError) {
	title := strings.TrimSpace(in.Title)
	errs = append(errs, validateBounds(FieldTitle, title, ml.Title)...)
	// Titles are single-line (00-overview.md §6): reject embedded newlines rather
	// than silently accepting a multi-line "title".
	if strings.ContainsAny(title, "\r\n") {
		errs = append(errs, FieldError{Field: FieldTitle, Message: "title must be a single line"})
	}

	description := strings.TrimSpace(in.Description)
	errs = append(errs, validateBounds(FieldDescription, description, ml.Description)...)

	// Tag rules (count + per-tag length + normalization) live in one place; map
	// every violation onto the tags field.
	normTags, err := tags.Validate(tl, in.Tags)
	var verr *tags.ValidationError
	if errors.As(err, &verr) {
		for _, v := range verr.Violations {
			errs = append(errs, FieldError{Field: FieldTags, Message: v})
		}
	}

	return normTags, errs
}

// validateBounds checks a field's rune length against its bounds, producing a
// human-readable message (empty value → "required"; otherwise the range).
func validateBounds(field, value string, b Bounds) []FieldError {
	n := utf8.RuneCountInString(value)
	if n == 0 {
		return []FieldError{{Field: field, Message: fmt.Sprintf("%s is required", field)}}
	}
	if n < b.Min || n > b.Max {
		return []FieldError{{
			Field:   field,
			Message: fmt.Sprintf("%s must be %d–%d characters (got %d)", field, b.Min, b.Max, n),
		}}
	}
	return nil
}
