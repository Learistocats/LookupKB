package server

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/Learistocats/LookupKB/api/internal/access"
	"github.com/Learistocats/LookupKB/api/internal/search"
	"github.com/gofiber/fiber/v2"
)

// maxQueryRunes bounds the query string forwarded to the search engine.
const maxQueryRunes = 200

// SearchService runs access-filtered search. *search.Service satisfies it.
type SearchService interface {
	Search(ctx context.Context, q search.Query, caller access.Caller) (search.Result, error)
}

// handleSearch serves GET /api/search for the UI:
//
//	?q=&limit=                      free text + page size (unchanged shape)
//	&facets=tags                    tag facet counts (LOO-34)
//	&tags=a,b (or repeated tags=)   entry must carry every listed tag
//	&author_id=<uuid>               one author
//	&published_from=&published_to=  published_at range (RFC 3339 or YYYY-MM-DD)
//
// The access filter is derived from the verified caller and injected server-side
// (protected tags, LOO-33); the client filters above are an allowlist ANDed to it,
// so they can only narrow the result — never widen it (05-search-and-indexing.md
// §3). Any other parameter is ignored; an invalid filter value is a 400.
func (s *Server) handleSearch(c *fiber.Ctx) error {
	caller, err := s.accessCaller(c)
	if err != nil {
		return err
	}
	q := []rune(c.Query("q"))
	if len(q) > maxQueryRunes {
		q = q[:maxQueryRunes]
	}
	limit, _ := strconv.Atoi(c.Query("limit"))

	query := search.Query{
		Q:      string(q),
		Limit:  limit,
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
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "search is unavailable"})
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
