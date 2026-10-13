package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// DocumentStore is the subset of the generated queries the document routes need.
// *dbgen.Queries satisfies it; kept as an interface so the handlers are testable
// without a database. The draft queries are owner-scoped (author_id) and never
// touch versions/index/status — see internal/db/queries/documents.sql.
//
// SetRenderedHTML is the write path to documents.rendered_html for the sanitized
// publish-time HTML (LOO-16). It has no handler yet: the publish gate (LOO-14)
// composes render.RenderAndSanitize output into its publish transaction and calls
// it there. The seam ships now so that ticket has nothing to retrofit here.
type DocumentStore interface {
	CreateDraft(ctx context.Context, arg dbgen.CreateDraftParams) (dbgen.Document, error)
	SaveDraft(ctx context.Context, arg dbgen.SaveDraftParams) (dbgen.Document, error)
	GetDraft(ctx context.Context, arg dbgen.GetDraftParams) (dbgen.Document, error)
	ListDrafts(ctx context.Context, authorID pgtype.UUID) ([]dbgen.ListDraftsRow, error)
	ListMyDocuments(ctx context.Context, authorID pgtype.UUID) ([]dbgen.ListMyDocumentsRow, error)
	ListDocumentTagLabels(ctx context.Context, documentID pgtype.UUID) ([]string, error)
	SetRenderedHTML(ctx context.Context, arg dbgen.SetRenderedHTMLParams) error
	// GetPublishedBySlug is the hot read path (F8): a published doc by slug, NOT
	// owner-scoped (public reads), with author/version/tags joined. status guard
	// makes a draft/unpublished slug indistinguishable from a missing one (404).
	GetPublishedBySlug(ctx context.Context, slug string) (dbgen.GetPublishedBySlugRow, error)
}

// draftResponse is the JSON projection returned to the caller. Only the fields
// the walking-skeleton UI needs are exposed; rendered_html, versioning and
// search_tsv are publish-time/M1 concerns and stay server-side.
type draftResponse struct {
	ID          string  `json:"id"`
	Slug        string  `json:"slug"`
	Status      string  `json:"status"`
	Title       *string `json:"title"`
	Description *string `json:"description"`
	SourceMd    *string `json:"source_md"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	// Tags are the labels attached at the last publish (only on GET).
	Tags []string `json:"tags,omitempty"`
}

func toDraftResponse(d dbgen.Document) draftResponse {
	return draftResponse{
		ID:          formatUUID(d.ID),
		Slug:        d.Slug,
		Status:      string(d.Status),
		Title:       d.Title,
		Description: d.Description,
		SourceMd:    d.SourceMd,
		CreatedAt:   formatTime(d.CreatedAt),
		UpdatedAt:   formatTime(d.UpdatedAt),
	}
}

// draftListItem is the per-row projection for the private workspace list. It is
// deliberately lighter than draftResponse — title (nullable; the UI renders
// "Untitled" when absent) and updated_at are all the list needs; source_md and
// rendered_html are never listed.
type draftListItem struct {
	ID        string  `json:"id"`
	Slug      string  `json:"slug"`
	Status    string  `json:"status"`
	Title     *string `json:"title"`
	UpdatedAt string  `json:"updated_at"`
}

func toDraftListItem(r dbgen.ListDraftsRow) draftListItem {
	return draftListItem{
		ID:        formatUUID(r.ID),
		Slug:      r.Slug,
		Status:    string(r.Status),
		Title:     r.Title,
		UpdatedAt: formatTime(r.UpdatedAt),
	}
}

// handleCreateDraft creates an empty draft owned by the caller (AC-2.1): a new
// document with status 'draft' and no metadata required. The slug is a
// server-generated placeholder (schema requires slug NOT NULL UNIQUE); the real
// title-derived slug is assigned at publish (M1), so it is not exposed here.
func (s *Server) handleCreateDraft(c *fiber.Ctx) error {
	authorID, err := s.callerID(c)
	if err != nil {
		return err
	}

	slug, err := newDraftSlug()
	if err != nil {
		s.log.ErrorContext(c.UserContext(), "draft slug generation failed", "error", err.Error())
		return internalError(c)
	}

	// Title/description/source_md left nil — creating a draft requires no metadata.
	doc, err := s.documents.CreateDraft(c.UserContext(), dbgen.CreateDraftParams{
		Slug:     slug,
		AuthorID: authorID,
	})
	if err != nil {
		s.log.ErrorContext(c.UserContext(), "create draft failed", "error", err.Error())
		return internalError(c)
	}

	return c.Status(fiber.StatusCreated).JSON(toDraftResponse(doc))
}

// handleSaveDraft overwrites the draft's working copy in place (AC-2.2): title,
// description and source_md are all optional and empty/partial metadata saves
// with no validation gate (that gate is publish-time). The query is scoped to
// author_id, so a non-owner yields no row → 404, never a cross-author write. A
// published entry may be saved too: only its working copy changes until the
// author re-publishes.
func (s *Server) handleSaveDraft(c *fiber.Ctx) error {
	docID, ok := parseDocID(c)
	if !ok {
		return badRequest(c, "invalid document id")
	}
	authorID, err := s.callerID(c)
	if err != nil {
		return err
	}

	var body struct {
		Title       *string `json:"title"`
		Description *string `json:"description"`
		SourceMd    *string `json:"source_md"`
	}
	if err := c.BodyParser(&body); err != nil {
		return badRequest(c, "invalid request body")
	}

	doc, err := s.documents.SaveDraft(c.UserContext(), dbgen.SaveDraftParams{
		ID:          docID,
		AuthorID:    authorID,
		Title:       body.Title,
		Description: body.Description,
		SourceMd:    body.SourceMd,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return notFound(c)
	}
	if err != nil {
		s.log.ErrorContext(c.UserContext(), "save draft failed", "error", err.Error())
		return internalError(c)
	}

	return c.JSON(toDraftResponse(doc))
}

// handleGetDraft returns the caller's own draft. GetDraft is scoped to
// author_id, so another user's draft (or a missing/soft-deleted one) yields no
// row → 404. The not-found response is identical for "not yours" and "does not
// exist" so ownership is never leaked to a probing second user.
func (s *Server) handleGetDraft(c *fiber.Ctx) error {
	docID, ok := parseDocID(c)
	if !ok {
		return badRequest(c, "invalid document id")
	}
	authorID, err := s.callerID(c)
	if err != nil {
		return err
	}

	doc, err := s.documents.GetDraft(c.UserContext(), dbgen.GetDraftParams{
		ID:       docID,
		AuthorID: authorID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return notFound(c)
	}
	if err != nil {
		s.log.ErrorContext(c.UserContext(), "get draft failed", "error", err.Error())
		return internalError(c)
	}

	tags, err := s.documents.ListDocumentTagLabels(c.UserContext(), doc.ID)
	if err != nil {
		s.log.ErrorContext(c.UserContext(), "list document tags failed", "error", err.Error())
		return internalError(c)
	}
	resp := toDraftResponse(doc)
	resp.Tags = tags
	if resp.Tags == nil {
		resp.Tags = []string{}
	}
	return c.JSON(resp)
}

// handleListMyDocuments returns the caller's own entries for the history panel:
// drafts first, then the rest by most recent edit. Owner-scoped like every
// draft query; an empty list is [] (never null).
func (s *Server) handleListMyDocuments(c *fiber.Ctx) error {
	authorID, err := s.callerID(c)
	if err != nil {
		return err
	}
	rows, err := s.documents.ListMyDocuments(c.UserContext(), authorID)
	if err != nil {
		s.log.ErrorContext(c.UserContext(), "list my documents failed", "error", err.Error())
		return internalError(c)
	}
	items := make([]draftListItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, draftListItem{
			ID:        formatUUID(r.ID),
			Slug:      r.Slug,
			Status:    string(r.Status),
			Title:     r.Title,
			UpdatedAt: formatTime(r.UpdatedAt),
		})
	}
	return c.JSON(items)
}

// handleListDrafts returns the caller's own drafts for their private workspace
// (AC-2.5). The query is owner-scoped (author_id) and status='draft', so no other
// user's drafts are ever returned (AC-2.3) — admins included: there is no bypass,
// a deliberate privacy decision (03-data-model.md §2). An empty workspace returns
// a JSON [] (never null) so the client can render uniformly.
func (s *Server) handleListDrafts(c *fiber.Ctx) error {
	authorID, err := s.callerID(c)
	if err != nil {
		return err
	}

	rows, err := s.documents.ListDrafts(c.UserContext(), authorID)
	if err != nil {
		s.log.ErrorContext(c.UserContext(), "list drafts failed", "error", err.Error())
		return internalError(c)
	}

	items := make([]draftListItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, toDraftListItem(r))
	}
	return c.JSON(items)
}

// callerID returns the verified caller's internal user id as a pgtype.UUID for
// use as author_id. The auth middleware guarantees a valid id on protected
// routes, so an unauthenticated ctx or an unparseable id is a server-side
// invariant break (500), not a client error.
func (s *Server) callerID(c *fiber.Ctx) (pgtype.UUID, error) {
	id, ok := auth.IdentityFromCtx(c)
	if !ok {
		return pgtype.UUID{}, c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}
	uid, err := parseUUID(id.UserID)
	if err != nil {
		s.log.ErrorContext(c.UserContext(), "caller has no valid internal id", "error", err.Error())
		return pgtype.UUID{}, internalError(c)
	}
	return uid, nil
}

// parseDocID reads and validates the :id path parameter as a uuid.
func parseDocID(c *fiber.Ctx) (pgtype.UUID, bool) {
	id, err := parseUUID(c.Params("id"))
	if err != nil {
		return pgtype.UUID{}, false
	}
	return id, true
}

// newDraftSlug returns a collision-resistant placeholder slug ("draft-" + 16 hex
// chars). 64 bits of randomness makes a UNIQUE violation on documents.slug
// astronomically unlikely for M0's single-draft-per-create flow.
func newDraftSlug() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "draft-" + hex.EncodeToString(b[:]), nil
}

func parseUUID(s string) (pgtype.UUID, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return pgtype.UUID{}, err
	}
	return pgtype.UUID{Bytes: u, Valid: true}, nil
}

func formatUUID(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return uuid.UUID(u.Bytes).String()
}

func formatTime(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.UTC().Format(time.RFC3339)
}

func badRequest(c *fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": msg})
}

func notFound(c *fiber.Ctx) error {
	return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "draft not found"})
}

func internalError(c *fiber.Ctx) error {
	return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal error"})
}
