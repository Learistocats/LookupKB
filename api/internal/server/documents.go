package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"

	"github.com/Learistocats/LookupKB/api/internal/access"
	"github.com/Learistocats/LookupKB/api/internal/db"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// DocumentStore is the subset of the generated queries the document routes need.
// *dbgen.Queries satisfies it; kept as an interface so the handlers are testable
// without a database. The draft queries are owner-scoped (author_id).
type DocumentStore interface {
	CreateDraft(ctx context.Context, arg dbgen.CreateDraftParams) (dbgen.Document, error)
	SaveDraft(ctx context.Context, arg dbgen.SaveDraftParams) (dbgen.Document, error)
	GetOwnedDocument(ctx context.Context, arg dbgen.GetOwnedDocumentParams) (dbgen.Document, error)
	ListDrafts(ctx context.Context, authorID pgtype.UUID) ([]dbgen.ListDraftsRow, error)
	ListMyDocuments(ctx context.Context, authorID pgtype.UUID) ([]dbgen.ListMyDocumentsRow, error)
	ListDocumentTagLabels(ctx context.Context, documentID pgtype.UUID) ([]string, error)
	// GetPublishedBySlug is not owner-scoped; its status guard makes a
	// draft/unpublished slug indistinguishable from a missing one.
	GetPublishedBySlug(ctx context.Context, slug string) (dbgen.GetPublishedBySlugRow, error)
	// ListPublishedDocuments filters by access in SQL before the LIMIT.
	ListPublishedDocuments(ctx context.Context, arg dbgen.ListPublishedDocumentsParams) ([]dbgen.ListPublishedDocumentsRow, error)
	// The protected-tag policy source for the read check.
	access.Querier
}

// draftResponse is the owner's view of a document: the working copy, never
// rendered_html.
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

// draftListItem is the light per-row projection for the owner's lists (no bodies).
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

// handleCreateDraft creates an empty draft owned by the caller. The slug is a
// placeholder (the column is NOT NULL UNIQUE); the real one is assigned at publish.
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

// handleSaveDraft overwrites the working copy in place with no validation gate
// (that is publish-time). The query is scoped to author_id, so a non-owner gets
// 404, never a cross-author write. A published entry may be saved too: only its
// working copy changes until re-publish.
func (s *Server) handleSaveDraft(c *fiber.Ctx) error {
	docID, ok := parseUUIDParam(c, "id")
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
		return notFound(c, "draft")
	}
	if err != nil {
		s.log.ErrorContext(c.UserContext(), "save draft failed", "error", err.Error())
		return internalError(c)
	}

	return c.JSON(toDraftResponse(doc))
}

// handleGetDraft returns the caller's own draft. "Not yours" and "does not
// exist" give the identical 404 so ownership never leaks.
func (s *Server) handleGetDraft(c *fiber.Ctx) error {
	docID, ok := parseUUIDParam(c, "id")
	if !ok {
		return badRequest(c, "invalid document id")
	}
	authorID, err := s.callerID(c)
	if err != nil {
		return err
	}

	doc, err := s.documents.GetOwnedDocument(c.UserContext(), dbgen.GetOwnedDocumentParams{
		ID:       docID,
		AuthorID: authorID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return notFound(c, "draft")
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
	resp.Tags = db.NonNil(tags)
	return c.JSON(resp)
}

// handleListMyDocuments returns the caller's entries for the history panel:
// drafts first, then by most recent edit.
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
		items = append(items, toDraftListItem(dbgen.ListDraftsRow(r)))
	}
	return c.JSON(items)
}

// handleListDrafts returns the caller's own drafts. There is no admin bypass:
// other users' drafts are never visible. Empty is [] (never null).
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

// callerID returns the verified caller's user id (e.g. as author_id). Without an
// identity it writes the 401 itself and returns it as the error.
func (s *Server) callerID(c *fiber.Ctx) (pgtype.UUID, error) {
	caller, err := s.accessCaller(c)
	return caller.UserID, err
}

// newDraftSlug returns a placeholder slug ("draft-" + 16 hex chars); 64 random
// bits make a UNIQUE collision negligible.
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

var (
	formatUUID = db.FormatUUID
	formatTime = db.FormatTime
)

func errJSON(c *fiber.Ctx, status int, msg string) error {
	return c.Status(status).JSON(fiber.Map{"error": msg})
}

func badRequest(c *fiber.Ctx, msg string) error {
	return errJSON(c, fiber.StatusBadRequest, msg)
}

// notFound is the one 404 shape; callers name the resource, never the reason, so
// missing and denied look identical.
func notFound(c *fiber.Ctx, what string) error {
	return errJSON(c, fiber.StatusNotFound, what+" not found")
}

func internalError(c *fiber.Ctx) error {
	return errJSON(c, fiber.StatusInternalServerError, "internal error")
}

// queryInt reads an optional non-negative integer query parameter (0 when absent).
func queryInt(c *fiber.Ctx, name string) (int, error) {
	raw := c.Query(name)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid %s", name)
	}
	return n, nil
}
