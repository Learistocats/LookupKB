package publish

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Learistocats/LookupKB/api/internal/tags"
)

// Field names used in FieldError.Field; the frontend groups errors by these.
const (
	FieldTitle       = "title"
	FieldDescription = "description"
	FieldTags        = "tags"
	FieldContent     = "content"
)

// FieldError is one per-field validation failure, returned as a list in a 422.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// MetadataInput is the raw, client-supplied metadata of a publish attempt.
type MetadataInput struct {
	Title       string
	Description string
	Tags        []string
}

// ValidateMetadata checks a publish attempt's metadata against the current limits
// and returns the normalized tag set plus every per-field violation. It is pure
// and authoritative; client-side hints are never trusted. All fields are checked
// (not fail-fast) so the author sees every problem at once, and title/description
// are validated on their trimmed value.
func ValidateMetadata(ml MetadataLimits, tl tags.Limits, in MetadataInput) (normTags []string, errs []FieldError) {
	title := strings.TrimSpace(in.Title)
	errs = append(errs, validateBounds(FieldTitle, title, ml.Title)...)
	// Titles are single-line.
	if strings.ContainsAny(title, "\r\n") {
		errs = append(errs, FieldError{Field: FieldTitle, Message: "title must be a single line"})
	}

	description := strings.TrimSpace(in.Description)
	errs = append(errs, validateBounds(FieldDescription, description, ml.Description)...)

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
