package server

import (
	"context"
	"strconv"

	"github.com/Learistocats/LookupKB/api/internal/access"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/gofiber/fiber/v2"
)

// TagStore is the subset of the tag data access the autocomplete route needs.
// *tags.Store satisfies it; kept narrow so the handler is testable with a fake.
// The tag-apply/usage_count methods on *tags.Store are not here — they have no
// HTTP surface in this ticket (the publish gate calls them directly, LOO-14/17).
type TagStore interface {
	// SearchTags hides protected tags the caller is not granted (LOO-33).
	SearchTags(ctx context.Context, query string, limit int32, caller access.Caller) ([]dbgen.SearchTagsRow, error)
}

// tagResponse is the autocomplete projection: enough for the suggestion dropdown
// (slug to insert, label to show, usage_count for popularity ordering — AC-6.1).
// Protected tells the publish dialog that an entry carrying the tag is restricted
// to the groups granted it (LOO-33).
type tagResponse struct {
	Slug       string `json:"slug"`
	Label      string `json:"label"`
	UsageCount int32  `json:"usage_count"`
	Protected  bool   `json:"protected"`
}

// handleListTags serves the tag autocomplete (AC-6.1): GET /api/v1/tags?q=&limit=.
// An empty q returns the most-used tags. limit is optional (store clamps it). The
// route is under the authenticated group. Protected tags the caller lacks are
// excluded (their very existence is need-to-know); super-admins see every tag.
func (s *Server) handleListTags(c *fiber.Ctx) error {
	caller, err := s.accessCaller(c)
	if err != nil {
		return err
	}
	q := c.Query("q")

	// limit is advisory; a malformed value falls back to the store default rather
	// than erroring, so a typo in the typeahead URL never breaks suggestions.
	// ParseInt with bitSize 32 bounds the value to int32 range (no overflow).
	var limit int32
	if raw := c.Query("limit"); raw != "" {
		if n, err := strconv.ParseInt(raw, 10, 32); err == nil && n > 0 {
			limit = int32(n)
		}
	}

	rows, err := s.tags.SearchTags(c.UserContext(), q, limit, caller)
	if err != nil {
		s.log.ErrorContext(c.UserContext(), "tag search failed", "error", err.Error())
		return internalError(c)
	}

	out := make([]tagResponse, len(rows))
	for i, t := range rows {
		out[i] = tagResponse{Slug: t.Slug, Label: t.Label, UsageCount: t.UsageCount, Protected: t.IsProtected}
	}
	return c.JSON(out)
}
