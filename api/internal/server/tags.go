package server

import (
	"context"

	"github.com/Learistocats/LookupKB/api/internal/access"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/gofiber/fiber/v2"
)

// TagStore is the subset of the tag data access the autocomplete route needs.
// *tags.Store satisfies it; kept narrow so the handler is testable with a fake.
type TagStore interface {
	// SearchTags hides protected tags the caller is not granted.
	SearchTags(ctx context.Context, query string, limit int32, caller access.Caller) ([]dbgen.SearchTagsRow, error)
}

// tagResponse is the autocomplete projection. Protected tells the publish dialog
// that an entry carrying the tag is restricted to the groups granted it.
type tagResponse struct {
	Slug       string `json:"slug"`
	Label      string `json:"label"`
	UsageCount int32  `json:"usage_count"`
	Protected  bool   `json:"protected"`
}

// handleListTags serves GET /api/v1/tags?q=&limit= (?prefix= aliases q). An empty
// q returns the most-used tags; the store clamps limit. Protected tags the caller
// lacks are excluded (their existence is need-to-know); super-admins see all.
func (s *Server) handleListTags(c *fiber.Ctx) error {
	caller, err := s.accessCaller(c)
	if err != nil {
		return err
	}
	q := c.Query("q")
	if q == "" {
		q = c.Query("prefix")
	}

	limit, err := queryInt(c, "limit")
	if err != nil {
		return badRequest(c, err.Error())
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
