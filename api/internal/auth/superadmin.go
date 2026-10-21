package auth

import (
	"context"
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// SuperAdminLoader reads a user's users.is_super_admin flag. *dbgen.Queries
// satisfies it. It is an optional capability of the store passed to
// NewMiddleware as its GroupLister: when that store also implements
// SuperAdminLoader the flag is loaded on every request; otherwise (nil store, or
// a store without it) every caller is a non-admin — deny-by-default.
type SuperAdminLoader interface {
	GetUserIsSuperAdmin(ctx context.Context, userID pgtype.UUID) (bool, error)
}

// loadSuperAdmin resolves the caller's super-admin flag from the database. It is
// re-checked per request, never trusted from the token, so demoting an admin takes
// effect on their next request (06-security.md, privilege escalation). A token
// whose subject no longer exists is simply not an admin.
func (m *Middleware) loadSuperAdmin(ctx context.Context, uid uuid.UUID) (bool, error) {
	loader, ok := m.groups.(SuperAdminLoader)
	if !ok {
		return false, nil
	}
	is, err := loader.GetUserIsSuperAdmin(ctx, pgtype.UUID{Bytes: uid, Valid: true})
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
