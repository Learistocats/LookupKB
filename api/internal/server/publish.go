package server

import (
	"context"
	"errors"

	"github.com/Learistocats/LookupKB/api/internal/db"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/Learistocats/LookupKB/api/internal/publish"
	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgtype"
)

// PublishService is the subset of the publish orchestrator the routes need. The
// concrete *publish.Service satisfies it; kept as an interface so the handlers are
// testable with a fake (no database/renderer), mirroring DocumentStore/TagStore.
type PublishService interface {
	Publish(ctx context.Context, in publish.Input) (*publish.Result, []publish.FieldError, error)
	Unpublish(ctx context.Context, docID, authorID pgtype.UUID) (*publish.Result, error)
}

// publishRequest is the metadata to publish with. There is no access field (it
// follows from the tags) and no source_md: the server publishes its own working
// copy.
type publishRequest struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}

// publishedResponse projects a published/unpublished document back to the caller.
type publishedResponse struct {
	ID          string  `json:"id"`
	Slug        string  `json:"slug"`
	Status      string  `json:"status"`
	Title       *string `json:"title"`
	Description *string `json:"description"`
	PublishedAt string  `json:"published_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func toPublishedResponse(d dbgen.Document) publishedResponse {
	return publishedResponse{
		ID:          db.FormatUUID(d.ID),
		Slug:        d.Slug,
		Status:      string(d.Status),
		Title:       d.Title,
		Description: d.Description,
		PublishedAt: db.FormatTime(d.PublishedAt),
		UpdatedAt:   db.FormatTime(d.UpdatedAt),
	}
}

// handlePublish is the metadata-gated publish action. Validation is server-side:
// a violation returns 422 with per-field errors and the document is unchanged.
func (s *Server) handlePublish(c *fiber.Ctx) error {
	docID, ok := parseUUIDParam(c, "id")
	if !ok {
		return badRequest(c, "invalid document id")
	}
	caller, err := s.accessCaller(c)
	if err != nil {
		return err
	}

	var body publishRequest
	if err := c.BodyParser(&body); err != nil {
		return badRequest(c, "invalid request body")
	}

	result, fieldErrs, err := s.publish.Publish(c.UserContext(), publish.Input{
		DocID:        docID,
		AuthorID:     caller.UserID,
		IsSuperAdmin: caller.IsSuperAdmin,
		Title:        body.Title,
		Description:  body.Description,
		Tags:         body.Tags,
	})
	if errors.Is(err, publish.ErrNotFound) {
		return notFound(c, "document")
	}
	if err != nil {
		s.log.ErrorContext(c.UserContext(), "publish failed", "error", err.Error())
		return internalError(c)
	}
	if len(fieldErrs) > 0 {
		// Per-field errors so the UI renders each inline.
		return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{"errors": fieldErrs})
	}

	return c.JSON(toPublishedResponse(result.Document))
}

// handleUnpublish reverts a published document to author-only visibility. Only
// the owner of a published doc succeeds; anything else gets the same 404.
func (s *Server) handleUnpublish(c *fiber.Ctx) error {
	docID, ok := parseUUIDParam(c, "id")
	if !ok {
		return badRequest(c, "invalid document id")
	}
	authorID, err := s.callerID(c)
	if err != nil {
		return err
	}

	result, err := s.publish.Unpublish(c.UserContext(), docID, authorID)
	if errors.Is(err, publish.ErrNotFound) {
		return notFound(c, "document")
	}
	if err != nil {
		s.log.ErrorContext(c.UserContext(), "unpublish failed", "error", err.Error())
		return internalError(c)
	}

	return c.JSON(toPublishedResponse(result.Document))
}
