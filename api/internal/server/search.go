package server

import (
	"context"
	"errors"
	"strings"

	"github.com/Learistocats/LookupKB/api/internal/access"
	"github.com/Learistocats/LookupKB/api/internal/search"
	"github.com/gofiber/fiber/v2"
)

// SearchService runs access-filtered search. *search.Service satisfies it.
type SearchService interface {
	Search(ctx context.Context, q search.Query, caller access.Caller) (search.Result, error)
}

// handleSearch serves GET /api/search (UI) and /api/v1/search (public API):
//
//	?q=&limit=                      free text + page size
//	&facets=tags                    tag facet counts
//	&tags=a,b (or repeated tags=)   entry must carry every listed tag
//	&author_id=<uuid>               one author
//	&published_from=&published_to=  published_at range (RFC 3339 or YYYY-MM-DD)
//	&offset=                        pagination (0..search.MaxOffset); the
//	                                response carries next_offset when more exist
//
// The access filter is derived from the verified caller and injected
// server-side; the client filters are ANDed to it, so they can only narrow the
// result. Other parameters are ignored; an invalid value is a 400.
func (s *Server) handleSearch(c *fiber.Ctx) error {
	caller, err := s.accessCaller(c)
	if err != nil {
		return err
	}
	limit, err := queryInt(c, "limit")
	if err != nil {
		return badRequest(c, err.Error())
	}
	offset, err := queryInt(c, "offset")
	if err != nil {
		return badRequest(c, err.Error())
	}

	query := search.Query{
		Q:      c.Query("q"),
		Limit:  int(limit),
		Offset: int(offset),
		Facets: multiValue(c, "facets"),
		Filters: search.Filters{
			Tags:          multiValue(c, "tags"),
			AuthorID:      c.Query("author_id"),
			PublishedFrom: c.Query("published_from"),
			PublishedTo:   c.Query("published_to"),
		},
	}

	res, err := s.search.Search(c.UserContext(), query, caller)
	if errors.Is(err, search.ErrInvalidFilter) {
		return badRequest(c, "invalid search filter")
	}
	if err != nil {
		s.log.ErrorContext(c.UserContext(), "search failed", "error", err.Error())
		return errJSON(c, fiber.StatusServiceUnavailable, "search is unavailable")
	}
	return c.JSON(res)
}

// multiValue reads a list parameter given as repeated keys and/or comma-separated
// values (?tags=a&tags=b or ?tags=a,b).
func multiValue(c *fiber.Ctx, key string) []string {
	var out []string
	for _, raw := range c.Context().QueryArgs().PeekMulti(key) {
		for _, v := range strings.Split(string(raw), ",") {
			if v = strings.TrimSpace(v); v != "" {
				out = append(out, v)
			}
		}
	}
	return out
}
