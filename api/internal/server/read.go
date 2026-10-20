package server

import (
	"errors"

	"github.com/Learistocats/LookupKB/api/internal/access"
	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
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

func toReadResponse(row dbgen.GetPublishedBySlugRow) readResponse {
	tags := row.Tags
	if tags == nil {
		tags = []string{}
	}
	return readResponse{
		Slug:              row.Slug,
		Title:             row.Title,
		Description:       row.Description,
		AuthorDisplayName: &row.AuthorDisplayName,
		PublishedAt:       formatTime(row.PublishedAt),
		Tags:              tags,
		RenderedHTML:      *row.RenderedHtml,
	}
}

// handleReadDocument serves a published document's cached, sanitized HTML by slug
// (F8, AC-8.1). It is intentionally thin: a straight column read — NO Markdown
// parsing or diagram rendering on this path (that happened once at publish).
//
// Read access is the protected-tag rule (access.Policy.CanRead, LOO-33):
// super-admin, author, or granted every protected tag the entry carries.
//
// Not-found, not-published, soft-deleted, and access-denied all return the SAME 404
// so a probing caller can never distinguish "doesn't exist" from "exists but denied"
// (AC-8.4, no content/existence/timing leak).
func (s *Server) handleReadDocument(c *fiber.Ctx) error {
	ctx := c.UserContext()

	slug := c.Params("slug")
	if slug == "" {
		return badRequest(c, "invalid slug")
	}

	caller, err := s.accessCaller(c)
	if err != nil {
		return err
	}
	// The protected-tag policy (LOO-33) is resolved BEFORE the lookup, for every
	// request, so "missing" and "denied" cost the same work (no timing oracle).
	policy, err := access.Load(ctx, s.documents, caller)
	if err != nil {
		s.log.ErrorContext(ctx, "load access policy failed", "error", err.Error())
		return internalError(c)
	}

	row, err := s.documents.GetPublishedBySlug(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return documentNotFound(c)
	}
	if err != nil {
		s.log.ErrorContext(ctx, "read document failed", "error", err.Error())
		return internalError(c)
	}

	if !policy.CanRead(row.AuthorID, row.TagSlugs) {
		// Identical body/status to the not-found case above — never leak existence.
		return documentNotFound(c)
	}

	if row.RenderedHtml == nil {
		// A published document must always have cached rendered_html (publish renders
		// it). A missing value is a data-integrity bug to surface/alert on — NOT a
		// "render just this once" fallback bolted onto the hot read path.
		s.log.ErrorContext(ctx, "published document missing rendered_html", "slug", slug)
		return internalError(c)
	}

	resp := toReadResponse(row)
	if caller.UserID == row.AuthorID {
		resp.ID = formatUUID(row.ID)
	}
	return c.JSON(resp)
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
