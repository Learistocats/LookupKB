package server

import (
	"context"
	"strconv"

	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/Learistocats/LookupKB/api/internal/search"
	"github.com/gofiber/fiber/v2"
)

// maxQueryRunes bounds the query string forwarded to the search engine.
const maxQueryRunes = 200

// SearchService runs access-filtered search. *search.Service satisfies it.
type SearchService interface {
	Search(ctx context.Context, q string, limit int, caller search.Caller) (search.Result, error)
}

// handleSearch serves GET /api/search?q=&limit= for the UI. The access filter is
// derived from the verified caller and injected server-side; the client cannot
// widen it (05-search-and-indexing.md §3).
func (s *Server) handleSearch(c *fiber.Ctx) error {
	id, ok := auth.IdentityFromCtx(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}
	q := []rune(c.Query("q"))
	if len(q) > maxQueryRunes {
		q = q[:maxQueryRunes]
	}
	limit, _ := strconv.Atoi(c.Query("limit"))

	res, err := s.search.Search(c.UserContext(), string(q), limit, search.Caller{UserID: id.UserID, Groups: id.Groups})
	if err != nil {
		s.log.ErrorContext(c.UserContext(), "search failed", "error", err.Error())
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "search is unavailable"})
	}
	return c.JSON(res)
}
