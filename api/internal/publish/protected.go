package publish

import (
	"context"
	"fmt"

	"github.com/Learistocats/LookupKB/api/internal/access"
)

// ProtectedTagErrors refuses tagging an entry with a protected tag the caller is
// not granted (LOO-33): one per-field "tags" error per offending tag, in the
// submitted order. A super-admin is never refused. The rule itself lives in
// internal/access; this only phrases its verdict for the 422.
//
// The publish gate runs it twice: before rendering (cheap early refusal) and again
// inside the publish transaction right before the tag apply, so an admin policy
// change landing between the two cannot slip a now-protected tag through.
func ProtectedTagErrors(ctx context.Context, q access.Querier, caller access.Caller, slugs []string) ([]FieldError, error) {
	if len(slugs) == 0 {
		return nil, nil
	}
	policy, err := access.Load(ctx, q, caller)
	if err != nil {
		return nil, err
	}
	var errs []FieldError
	for _, slug := range policy.DeniedAmong(slugs) {
		errs = append(errs, FieldError{
			Field:   FieldTags,
			Message: fmt.Sprintf("tag %q is protected; you don't have access to it", slug),
		})
	}
	return errs, nil
}
