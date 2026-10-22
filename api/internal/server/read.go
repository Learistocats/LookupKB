package server

import (
	"errors"

	"github.com/Learistocats/LookupKB/api/internal/access"
	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/Learistocats/LookupKB/api/internal/catalog"
	"github.com/Learistocats/LookupKB/api/internal/db"
	"github.com/gofiber/fiber/v2"
)

// readResponse is the published-safe projection for the reader: the pre-rendered
// sanitized HTML plus display metadata, never source_md, author_id or status.
type readResponse struct {
	// ID is set only when the caller is the author, so the UI can link to the
	// editor; other readers never learn the internal id.
	ID                string   `json:"id,omitempty"`
	Slug              string   `json:"slug"`
	Title             *string  `json:"title"`
	Description       *string  `json:"description"`
	AuthorDisplayName *string  `json:"author_display_name"`
	PublishedAt       string   `json:"published_at"`
	Tags              []string `json:"tags"`
	RenderedHTML      string   `json:"rendered_html"`
}

func toReadResponse(d catalog.Document) readResponse {
	title, desc, name := d.Title, d.Description, d.AuthorDisplayName
	return readResponse{
		Slug:              d.Slug,
		Title:             &title,
		Description:       &desc,
		AuthorDisplayName: &name,
		PublishedAt:       d.PublishedAt,
		Tags:              d.Tags,
		RenderedHTML:      d.HTML,
	}
}

// handleReadDocument serves a published document's cached, sanitized HTML by slug:
// a straight column read, no Markdown parsing on this path. Access (super-admin,
// author, or granted every protected tag) is decided in internal/catalog.
//
// Missing, unpublished, soft-deleted and denied all return the SAME 404 so a
// probe cannot tell them apart.
func (s *Server) handleReadDocument(c *fiber.Ctx) error {
	slug := c.Params("slug")
	if slug == "" {
		return badRequest(c, "invalid slug")
	}
	caller, err := s.accessCaller(c)
	if err != nil {
		return err
	}
	doc, err := s.readPublished(c, caller, slug)
	if err != nil {
		return err
	}
	resp := toReadResponse(doc)
	if caller.UserID == doc.AuthorID {
		resp.ID = db.FormatUUID(doc.ID)
	}
	return c.JSON(resp)
}

// readPublished resolves a readable published document through the catalog. A
// non-nil error means the response is already written: the identical 404 for
// missing/denied, 500 otherwise.
func (s *Server) readPublished(c *fiber.Ctx, caller access.Caller, slug string) (catalog.Document, error) {
	doc, err := s.catalog.Get(c.UserContext(), caller, slug)
	switch {
	case err == nil:
		return doc, nil
	case errors.Is(err, catalog.ErrNotFound):
		return catalog.Document{}, notFound(c, "document")
	case errors.Is(err, catalog.ErrIntegrity):
		// A data bug to alert on, never a render-on-read fallback.
		s.log.ErrorContext(c.UserContext(), "published document missing publish-time content", "slug", slug)
	default:
		s.log.ErrorContext(c.UserContext(), "read document failed", "error", err.Error())
	}
	return catalog.Document{}, internalError(c)
}

// accessCaller projects the verified identity onto an access.Caller. Without an
// identity it writes the 401 itself and returns it as the error.
func (s *Server) accessCaller(c *fiber.Ctx) (access.Caller, error) {
	id, ok := auth.IdentityFromCtx(c)
	if !ok {
		return access.Caller{}, errJSON(c, fiber.StatusUnauthorized, "unauthorized")
	}
	return id.Caller(), nil
}
