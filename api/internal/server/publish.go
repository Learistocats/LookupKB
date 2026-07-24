package server

import (
	"context"
	"errors"

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

// publishRequest is the confirm-flow body: the metadata to publish with. Access is
// omitted this milestone (documents default to public; restricted-group ACLs are
// M3). source_md is never sent — the server publishes its own held working copy.
type publishRequest struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}

// publishedResponse projects a published/unpublished document back to the caller.
// It extends draftResponse's fields with the publish-time metadata the UI shows:
// access scope, published_at, and (on publish) the new version number.
type publishedResponse struct {
	ID          string  `json:"id"`
	Slug        string  `json:"slug"`
	Status      string  `json:"status"`
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Access      string  `json:"access"`
	PublishedAt string  `json:"published_at"`
	UpdatedAt   string  `json:"updated_at"`
	VersionNo   int32   `json:"version_no,omitempty"`
}

func toPublishedResponse(d dbgen.Document, versionNo int32) publishedResponse {
	return publishedResponse{
		ID:          formatUUID(d.ID),
		Slug:        d.Slug,
		Status:      string(d.Status),
		Title:       d.Title,
		Description: d.Description,
		Access:      string(d.Access),
		PublishedAt: formatTime(d.PublishedAt),
		UpdatedAt:   formatTime(d.UpdatedAt),
		VersionNo:   versionNo,
	}
}

// handlePublish is the metadata-gated publish action (F5, AC-5.1–5.5). It validates
// server-side regardless of any client checks (06-security.md §6): on a metadata
// violation it returns 422 with per-field errors and the document stays a draft; on
// success the document is published (snapshot created, index op enqueued) atomically.
func (s *Server) handlePublish(c *fiber.Ctx) error {
	docID, ok := parseDocID(c)
	if !ok {
		return badRequest(c, "invalid document id")
	}
	authorID, err := s.callerID(c)
	if err != nil {
		return err
	}

	var body publishRequest
	if err := c.BodyParser(&body); err != nil {
		return badRequest(c, "invalid request body")
	}

	result, fieldErrs, err := s.publish.Publish(c.UserContext(), publish.Input{
		DocID:       docID,
		AuthorID:    authorID,
		Title:       body.Title,
		Description: body.Description,
		Tags:        body.Tags,
	})
	if errors.Is(err, publish.ErrNotFound) {
		return notFound(c)
	}
	if err != nil {
		s.log.ErrorContext(c.UserContext(), "publish failed", "error", err.Error())
		return internalError(c)
	}
	if len(fieldErrs) > 0 {
		// 422: the metadata gate blocked publish; the document is unchanged (draft).
		// Per-field errors (not a single blob) so the UI renders each inline.
		return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{"errors": fieldErrs})
	}

	return c.JSON(toPublishedResponse(result.Document, result.VersionNo))
}

// handleUnpublish reverts a published document to author-only visibility (AC-5.6).
// Only the owner of a currently-published doc succeeds; anything else is a 404 with
// the identical not-found body (no existence leak).
func (s *Server) handleUnpublish(c *fiber.Ctx) error {
	docID, ok := parseDocID(c)
	if !ok {
		return badRequest(c, "invalid document id")
	}
	authorID, err := s.callerID(c)
	if err != nil {
		return err
	}

	result, err := s.publish.Unpublish(c.UserContext(), docID, authorID)
	if errors.Is(err, publish.ErrNotFound) {
		return notFound(c)
	}
	if err != nil {
		s.log.ErrorContext(c.UserContext(), "unpublish failed", "error", err.Error())
		return internalError(c)
	}

	return c.JSON(toPublishedResponse(result.Document, 0))
}
