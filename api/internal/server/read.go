package server

import (
	"errors"

	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// CanRead decides read access to a PUBLISHED document — the single access-check
// seam for the read path (F8, 03-data-model.md §2). M1 scope is public-or-author:
//   - public docs are readable by any authenticated caller;
//   - an author always reads their own doc, whatever its access.
//
// This is deliberately the ONE function M3 (F10/AC-10.2) extends with the group
// intersection for restricted docs — add a branch here, don't rewrite call sites.
// callerGroups is already threaded through for that M3 branch.
func CanRead(callerID pgtype.UUID, callerGroups []string, access dbgen.DocumentAccess, authorID pgtype.UUID) bool {
	if access == dbgen.DocumentAccessPublic {
		return true
	}
	if callerID.Valid && callerID == authorID {
		return true
	}
	// TODO(M3, F10/AC-10.2): restricted && intersect(callerGroups, doc.acl_group_ids).
	return false
}

// readResponse is the published-safe projection returned by the read endpoint. It
// carries the pre-rendered sanitized HTML plus the display metadata the view shows
// (AC-8.5). It never exposes source_md, author_id, or the raw status — only the
// published projection a reader is allowed to see.
type readResponse struct {
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
		AuthorDisplayName: row.AuthorDisplayName,
		PublishedAt:       formatTime(row.PublishedAt),
		Tags:              tags,
		RenderedHTML:      *row.RenderedHtml,
	}
}

// handleReadDocument serves a published document's cached, sanitized HTML by slug
// (F8, AC-8.1). It is intentionally thin: a straight column read — NO Markdown
// parsing or diagram rendering on this path (that happened once at publish).
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

	callerID, err := s.callerID(c)
	if err != nil {
		return err
	}
	// Groups are needed for the M3 restricted-doc branch of CanRead; the caller's
	// identity is guaranteed present here because callerID succeeded.
	identity, _ := auth.IdentityFromCtx(c)

	row, err := s.documents.GetPublishedBySlug(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return documentNotFound(c)
	}
	if err != nil {
		s.log.ErrorContext(ctx, "read document failed", "error", err.Error())
		return internalError(c)
	}

	if !CanRead(callerID, identity.Groups, row.Access, row.AuthorID) {
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

	return c.JSON(toReadResponse(row))
}

// documentNotFound is the read path's 404. Its body is identical whether the doc is
// missing, unpublished, soft-deleted, or access-denied, so the two cases are
// indistinguishable to a probing caller (AC-8.4).
func documentNotFound(c *fiber.Ctx) error {
	return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "document not found"})
}
