package auth

import (
	"context"
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// loadSuperAdmin resolves a session caller's super-admin flag from the database,
// never from the token, so demoting an admin takes effect on their next request.
// A token whose subject no longer exists is simply not an admin.
func (m *Middleware) loadSuperAdmin(ctx context.Context, uid uuid.UUID) (bool, error) {
	if m.store == nil {
		return false, nil
	}
	is, err := m.store.GetUserIsSuperAdmin(ctx, pgtype.UUID{Bytes: uid, Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return is, err
}

// RequireSuperAdmin is a Fiber handler guarding the /api/admin surface. It must
// run after Middleware.Handle; a request without a verified Identity, or whose
// caller is not a super-admin, gets 403.
func RequireSuperAdmin(c *fiber.Ctx) error {
	id, ok := IdentityFromCtx(c)
	if !ok || !id.IsSuperAdmin {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "forbidden"})
	}
	return c.Next()
}
