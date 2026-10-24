package server

import (
	"errors"

	"github.com/Learistocats/LookupKB/api/internal/catalog"
	"github.com/gofiber/fiber/v2"
)

// The versioned /api/v1 surface for programmatic callers (a PAT, or the BFF's
// session): read-only, published entries only, access-filtered for the verified
// caller by the same policy as the UI.

// v1Document is the JSON envelope of GET /api/v1/documents/{slug}: the public
// metadata plus the content in both stored representations. Internal ids, the
// author id and the working copy are never part of it.
type v1Document struct {
	catalog.Summary
	HTML     string `json:"html"`
	Markdown string `json:"markdown"`
}

// Media types offered by GET /api/v1/documents/{slug}, in preference order for a
// wildcard Accept (JSON first).
var documentMediaTypes = []string{
	fiber.MIMEApplicationJSON,
	"text/markdown",
	fiber.MIMETextHTML,
	fiber.MIMETextPlain,
}

// handleV1ListDocuments serves GET /api/v1/documents?limit=&cursor=: published
// entries the caller may read, newest first, metadata only. The protected-tag
// rule is applied in SQL before the LIMIT, so pages are never short and never
// hint at a hidden entry.
func (s *Server) handleV1ListDocuments(c *fiber.Ctx) error {
	caller, err := s.accessCaller(c)
	if err != nil {
		return err
	}
	limit, err := queryInt(c, "limit")
	if err != nil {
		return badRequest(c, err.Error())
	}
	page, err := s.catalog.List(c.UserContext(), caller, int(limit), c.Query("cursor"))
	if errors.Is(err, catalog.ErrInvalidCursor) {
		return badRequest(c, "invalid cursor")
	}
	if err != nil {
		s.log.ErrorContext(c.UserContext(), "list published documents failed", "error", err.Error())
		return internalError(c)
	}
	return c.JSON(page)
}

// handleV1GetDocument serves GET /api/v1/documents/{slug} in the representation
// chosen by ?format=json|html|text|markdown, else by Accept (application/json,
// text/html, text/plain, text/markdown), else JSON. Missing and denied slugs get
// the identical 404 (the catalog resolves the policy first).
//
//   - html: the cached rendered_html, sanitized once at publish — served verbatim.
//   - text: plain text extracted from that HTML (the indexer's extraction).
//   - markdown: published_md, the source that HTML was rendered from — never the
//     working copy, which may hold unpublished edits.
func (s *Server) handleV1GetDocument(c *fiber.Ctx) error {
	var format catalog.Format
	if raw := c.Query("format"); raw != "" {
		f, ok := catalog.ParseFormat(raw)
		if !ok {
			return badRequest(c, "invalid format")
		}
		format = f
	} else {
		format = formatFromAccept(c.Accepts(documentMediaTypes...))
	}
	// The representation depends on Accept when ?format is absent.
	c.Vary(fiber.HeaderAccept)

	caller, err := s.accessCaller(c)
	if err != nil {
		return err
	}
	doc, err := s.readPublished(c, caller, c.Params("slug"))
	if err != nil {
		return err
	}

	if format == catalog.FormatJSON {
		return c.JSON(v1Document{Summary: doc.Summary, HTML: doc.HTML, Markdown: doc.Markdown})
	}

	switch format {
	case catalog.FormatHTML:
		c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
		// Author content, already sanitized; if a browser ever renders this
		// response directly, it runs nothing and loads nothing.
		c.Set(fiber.HeaderContentSecurityPolicy, "default-src 'none'; img-src * data:; style-src 'unsafe-inline'; sandbox")
	case catalog.FormatMarkdown:
		c.Set(fiber.HeaderContentType, "text/markdown; charset=utf-8")
	default:
		c.Set(fiber.HeaderContentType, fiber.MIMETextPlainCharsetUTF8)
	}
	c.Set(fiber.HeaderXContentTypeOptions, "nosniff")
	return c.SendString(doc.Body(format))
}

// formatFromAccept maps the negotiated media type to a format; anything else
// (no match) falls back to the JSON default.
func formatFromAccept(mediaType string) catalog.Format {
	switch mediaType {
	case "text/markdown":
		return catalog.FormatMarkdown
	case fiber.MIMETextHTML:
		return catalog.FormatHTML
	case fiber.MIMETextPlain:
		return catalog.FormatText
	}
	return catalog.FormatJSON
}
