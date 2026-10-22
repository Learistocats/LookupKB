package server

import (
	"context"
	"errors"

	"github.com/Learistocats/LookupKB/api/internal/drafts"
	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgtype"
)

// DraftDeleter is the transactional draft-delete operation the route needs. The
// concrete *drafts.Service satisfies it; kept as an interface so the handler is
// testable with a fake (no database), mirroring DocumentStore/PublishService.
type DraftDeleter interface {
	Delete(ctx context.Context, docID, authorID pgtype.UUID) error
}

// handleDeleteDraft hard-deletes one of the caller's own draft or unpublished
// entries and returns 204. A missing, other-author or published id gets
// the identical not-found body, so existence and ownership never leak; a
// published entry has to be unpublished first.
func (s *Server) handleDeleteDraft(c *fiber.Ctx) error {
	docID, ok := parseUUIDParam(c, "id")
	if !ok {
		return badRequest(c, "invalid document id")
	}
	authorID, err := s.callerID(c)
	if err != nil {
		return err
	}

	err = s.draftDelete.Delete(c.UserContext(), docID, authorID)
	if errors.Is(err, drafts.ErrNotFound) {
		return notFound(c, "draft")
	}
	if err != nil {
		s.log.ErrorContext(c.UserContext(), "delete draft failed", "error", err.Error())
		return internalError(c)
	}
	return c.SendStatus(fiber.StatusNoContent)
}
