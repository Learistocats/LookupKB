package server

import (
	"errors"

	"github.com/Learistocats/LookupKB/api/internal/access"
	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/Learistocats/LookupKB/api/internal/catalog"
	"github.com/gofiber/fiber/v2"
)

// readResponse is the published-safe projection returned by the read endpoint. It
// carries the pre-rendered sanitized HTML plus the display metadata the view shows
// (AC-8.5). It never exposes source_md, author_id, or the raw status — only the
// published projection a reader is allowed to see.
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

// handleReadDocument serves a published document's cached, sanitized HTML by slug
// (F8, AC-8.1). It is intentionally thin: a straight column read — NO Markdown
// parsing or diagram rendering on this path (that happened once at publish).
//
// Read access is the protected-tag rule (access.Policy.CanRead, LOO-33):
// super-admin, author, or granted every protected tag the entry carries. The
// lookup and the check live in internal/catalog, shared with the public API.
//
// Not-found, not-published, soft-deleted, and access-denied all return the SAME 404
// so a probing caller can never distinguish "doesn't exist" from "exists but denied"
// (AC-8.4, no content/existence/timing leak).
func (s *Server) handleReadDocument(c *fiber.Ctx) error {
	slug := c.Params("slug")
	if slug == "" {
		return badRequest(c, "invalid slug")
	}
	caller, err := s.accessCaller(c)
	if err != nil {
		return err
	}
	doc, ok, err := s.readPublished(c, caller, slug)
	if !ok {
		return err
	}
	resp := toReadResponse(doc)
	if caller.UserID == doc.AuthorID {
		resp.ID = formatUUID(doc.ID)
	}
	return c.JSON(resp)
}

// readPublished resolves a readable published document through the catalog. ok
// is false when it failed; the response is then already written — the identical
// 404 for missing/denied, 500 otherwise — and err is what the handler returns.
func (s *Server) readPublished(c *fiber.Ctx, caller access.Caller, slug string) (doc catalog.Document, ok bool, err error) {
	doc, err = s.catalog.Get(c.UserContext(), caller, slug)
	switch {
	case err == nil:
		return doc, true, nil
	case errors.Is(err, catalog.ErrNotFound):
		return catalog.Document{}, false, documentNotFound(c)
	case errors.Is(err, catalog.ErrIntegrity):
		// A published document must always carry its publish-time content. A
		// missing value is a data-integrity bug to surface/alert on — NOT a "render
		// just this once" fallback bolted onto the hot read path.
		s.log.ErrorContext(c.UserContext(), "published document missing publish-time content", "slug", slug)
		return catalog.Document{}, false, internalError(c)
	default:
		s.log.ErrorContext(c.UserContext(), "read document failed", "error", err.Error())
		return catalog.Document{}, false, internalError(c)
	}
}

// documentNotFound is the read path's 404. Its body is identical whether the doc is
// missing, unpublished, soft-deleted, or access-denied, so the two cases are
// indistinguishable to a probing caller (AC-8.4).
func documentNotFound(c *fiber.Ctx) error {
	return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "document not found"})
}

// accessCaller projects the verified identity onto an access.Caller (user id +
// the per-request super-admin flag). It writes the 401/500 response itself and
// returns it as the error, like callerID.
func (s *Server) accessCaller(c *fiber.Ctx) (access.Caller, error) {
	uid, err := s.callerID(c)
	if err != nil {
		return access.Caller{}, err
	}
	id, _ := auth.IdentityFromCtx(c) // present: callerID succeeded
	return access.Caller{UserID: uid, IsSuperAdmin: id.IsSuperAdmin}, nil
}
