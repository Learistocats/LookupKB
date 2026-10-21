package server

import (
	"context"
	"errors"

	"github.com/Learistocats/LookupKB/api/internal/admin"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgtype"
)

// AdminService is the super-admin management surface (LOO-56). *admin.Service
// satisfies it; the handlers here only map HTTP in/out and errors — the rules
// (confirm flows, last-super-admin guard, audit) live in internal/admin.
type AdminService interface {
	ListGroups(ctx context.Context) ([]dbgen.ListGroupsRow, error)
	GetGroup(ctx context.Context, id pgtype.UUID) (admin.GroupDetail, error)
	CreateGroup(ctx context.Context, actor pgtype.UUID, name string) (dbgen.Group, error)
	RenameGroup(ctx context.Context, actor, id pgtype.UUID, name string) (dbgen.Group, error)
	DeleteGroup(ctx context.Context, actor, id pgtype.UUID) error
	AddMember(ctx context.Context, actor, groupID pgtype.UUID, email string) (dbgen.User, error)
	RemoveMember(ctx context.Context, actor, groupID, userID pgtype.UUID) error
	GrantTag(ctx context.Context, actor, groupID pgtype.UUID, tag string, confirm bool) (dbgen.Tag, error)
	RevokeTag(ctx context.Context, actor, groupID, tagID pgtype.UUID, confirm bool) error
	ListUsers(ctx context.Context) ([]dbgen.ListUsersRow, error)
	SetSuperAdmin(ctx context.Context, actor, userID pgtype.UUID, value bool) (dbgen.SetUserSuperAdminRow, error)
}

type adminGroupResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	CreatedAt   string `json:"created_at"`
	MemberCount *int64 `json:"member_count,omitempty"`
	TagCount    *int64 `json:"tag_count,omitempty"`
}

type adminUserResponse struct {
	ID           string  `json:"id"`
	Email        string  `json:"email"`
	DisplayName  *string `json:"display_name"`
	IsSuperAdmin bool    `json:"is_super_admin"`
}

type adminTagGrantResponse struct {
	TagID     string `json:"tag_id"`
	Slug      string `json:"slug"`
	Label     string `json:"label"`
	CreatedAt string `json:"created_at,omitempty"`
}

type adminGroupDetailResponse struct {
	adminGroupResponse
	Members []adminUserResponse     `json:"members"`
	Tags    []adminTagGrantResponse `json:"tags"`
}

func toAdminGroup(g dbgen.Group) adminGroupResponse {
	return adminGroupResponse{ID: formatUUID(g.ID), Name: g.Name, CreatedAt: formatTime(g.CreatedAt)}
}

// registerAdminRoutes mounts /api/admin/* behind RequireSuperAdmin. The caller
// (registerRoutes) only invokes it when auth and the admin service are wired.
func (s *Server) registerAdminRoutes(adm fiber.Router) {
	adm.Get("/groups", s.handleAdminListGroups)
	adm.Post("/groups", s.handleAdminCreateGroup)
	adm.Get("/groups/:id", s.handleAdminGetGroup)
	adm.Patch("/groups/:id", s.handleAdminRenameGroup)
	adm.Delete("/groups/:id", s.handleAdminDeleteGroup)
	adm.Post("/groups/:id/members", s.handleAdminAddMember)
	adm.Delete("/groups/:id/members/:userId", s.handleAdminRemoveMember)
	adm.Post("/groups/:id/tags", s.handleAdminGrantTag)
	adm.Delete("/groups/:id/tags/:tagId", s.handleAdminRevokeTag)
	adm.Get("/users", s.handleAdminListUsers)
	adm.Put("/users/:id/super-admin", s.handleAdminSetSuperAdmin)
}

func (s *Server) handleAdminListGroups(c *fiber.Ctx) error {
	rows, err := s.admin.ListGroups(c.UserContext())
	if err != nil {
		return s.adminError(c, err)
	}
	out := make([]adminGroupResponse, len(rows))
	for i, r := range rows {
		mc, tc := r.MemberCount, r.TagCount
		out[i] = adminGroupResponse{
			ID: formatUUID(r.ID), Name: r.Name, CreatedAt: formatTime(r.CreatedAt),
			MemberCount: &mc, TagCount: &tc,
		}
	}
	return c.JSON(out)
}

type groupNameRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleAdminCreateGroup(c *fiber.Ctx) error {
	actor, err := s.callerID(c)
	if err != nil {
		return err
	}
	var body groupNameRequest
	if err := c.BodyParser(&body); err != nil {
		return badRequest(c, "invalid request body")
	}
	g, err := s.admin.CreateGroup(c.UserContext(), actor, body.Name)
	if err != nil {
		return s.adminError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toAdminGroup(g))
}

func (s *Server) handleAdminGetGroup(c *fiber.Ctx) error {
	id, ok := parseUUIDParam(c, "id")
	if !ok {
		return badRequest(c, "invalid group id")
	}
	d, err := s.admin.GetGroup(c.UserContext(), id)
	if err != nil {
		return s.adminError(c, err)
	}
	out := adminGroupDetailResponse{
		adminGroupResponse: toAdminGroup(d.Group),
		Members:            make([]adminUserResponse, len(d.Members)),
		Tags:               make([]adminTagGrantResponse, len(d.Tags)),
	}
	for i, m := range d.Members {
		out.Members[i] = adminUserResponse{ID: formatUUID(m.ID), Email: m.Email, DisplayName: m.DisplayName, IsSuperAdmin: m.IsSuperAdmin}
	}
	for i, t := range d.Tags {
		out.Tags[i] = adminTagGrantResponse{TagID: formatUUID(t.TagID), Slug: t.Slug, Label: t.Label, CreatedAt: formatTime(t.CreatedAt)}
	}
	return c.JSON(out)
}

func (s *Server) handleAdminRenameGroup(c *fiber.Ctx) error {
	actor, err := s.callerID(c)
	if err != nil {
		return err
	}
	id, ok := parseUUIDParam(c, "id")
	if !ok {
		return badRequest(c, "invalid group id")
	}
	var body groupNameRequest
	if err := c.BodyParser(&body); err != nil {
		return badRequest(c, "invalid request body")
	}
	g, err := s.admin.RenameGroup(c.UserContext(), actor, id, body.Name)
	if err != nil {
		return s.adminError(c, err)
	}
	return c.JSON(toAdminGroup(g))
}

func (s *Server) handleAdminDeleteGroup(c *fiber.Ctx) error {
	actor, err := s.callerID(c)
	if err != nil {
		return err
	}
	id, ok := parseUUIDParam(c, "id")
	if !ok {
		return badRequest(c, "invalid group id")
	}
	if err := s.admin.DeleteGroup(c.UserContext(), actor, id); err != nil {
		return s.adminError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

type addMemberRequest struct {
	Email string `json:"email"`
}

func (s *Server) handleAdminAddMember(c *fiber.Ctx) error {
	actor, err := s.callerID(c)
	if err != nil {
		return err
	}
	id, ok := parseUUIDParam(c, "id")
	if !ok {
		return badRequest(c, "invalid group id")
	}
	var body addMemberRequest
	if err := c.BodyParser(&body); err != nil {
		return badRequest(c, "invalid request body")
	}
	u, err := s.admin.AddMember(c.UserContext(), actor, id, body.Email)
	if err != nil {
		return s.adminError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(adminUserResponse{
		ID: formatUUID(u.ID), Email: u.Email, DisplayName: u.DisplayName, IsSuperAdmin: u.IsSuperAdmin,
	})
}

func (s *Server) handleAdminRemoveMember(c *fiber.Ctx) error {
	actor, err := s.callerID(c)
	if err != nil {
		return err
	}
	id, ok := parseUUIDParam(c, "id")
	if !ok {
		return badRequest(c, "invalid group id")
	}
	uid, ok := parseUUIDParam(c, "userId")
	if !ok {
		return badRequest(c, "invalid user id")
	}
	if err := s.admin.RemoveMember(c.UserContext(), actor, id, uid); err != nil {
		return s.adminError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

type grantTagRequest struct {
	Tag            string `json:"tag"`
	ConfirmProtect bool   `json:"confirm_protect"`
}

func (s *Server) handleAdminGrantTag(c *fiber.Ctx) error {
	actor, err := s.callerID(c)
	if err != nil {
		return err
	}
	id, ok := parseUUIDParam(c, "id")
	if !ok {
		return badRequest(c, "invalid group id")
	}
	var body grantTagRequest
	if err := c.BodyParser(&body); err != nil {
		return badRequest(c, "invalid request body")
	}
	t, err := s.admin.GrantTag(c.UserContext(), actor, id, body.Tag, body.ConfirmProtect)
	if err != nil {
		return s.adminError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(adminTagGrantResponse{TagID: formatUUID(t.ID), Slug: t.Slug, Label: t.Label})
}

func (s *Server) handleAdminRevokeTag(c *fiber.Ctx) error {
	actor, err := s.callerID(c)
	if err != nil {
		return err
	}
	id, ok := parseUUIDParam(c, "id")
	if !ok {
		return badRequest(c, "invalid group id")
	}
	tid, ok := parseUUIDParam(c, "tagId")
	if !ok {
		return badRequest(c, "invalid tag id")
	}
	confirm := c.QueryBool("confirm_unprotect", false)
	if err := s.admin.RevokeTag(c.UserContext(), actor, id, tid, confirm); err != nil {
		return s.adminError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *Server) handleAdminListUsers(c *fiber.Ctx) error {
	rows, err := s.admin.ListUsers(c.UserContext())
	if err != nil {
		return s.adminError(c, err)
	}
	out := make([]adminUserResponse, len(rows))
	for i, u := range rows {
		out[i] = adminUserResponse{ID: formatUUID(u.ID), Email: u.Email, DisplayName: u.DisplayName, IsSuperAdmin: u.IsSuperAdmin}
	}
	return c.JSON(out)
}

type superAdminRequest struct {
	// Value is a pointer so a missing field is a 400, not a silent revoke.
	Value *bool `json:"value"`
}

func (s *Server) handleAdminSetSuperAdmin(c *fiber.Ctx) error {
	actor, err := s.callerID(c)
	if err != nil {
		return err
	}
	uid, ok := parseUUIDParam(c, "id")
	if !ok {
		return badRequest(c, "invalid user id")
	}
	var body superAdminRequest
	if err := c.BodyParser(&body); err != nil || body.Value == nil {
		return badRequest(c, "invalid request body")
	}
	u, err := s.admin.SetSuperAdmin(c.UserContext(), actor, uid, *body.Value)
	if err != nil {
		return s.adminError(c, err)
	}
	return c.JSON(adminUserResponse{ID: formatUUID(u.ID), Email: u.Email, DisplayName: u.DisplayName, IsSuperAdmin: u.IsSuperAdmin})
}

// adminError maps the admin service's errors onto the API's JSON error shape.
func (s *Server) adminError(c *fiber.Ctx, err error) error {
	var verr *admin.ValidationError
	var cerr *admin.NeedsConfirmationError
	switch {
	case errors.As(err, &verr):
		return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{"error": verr.Message})
	case errors.As(err, &cerr):
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "needs_confirmation", "tag": cerr.Tag})
	case errors.Is(err, admin.ErrNotFound):
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "not found"})
	case errors.Is(err, admin.ErrGroupNameTaken):
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": admin.ErrGroupNameTaken.Error()})
	case errors.Is(err, admin.ErrLastSuperAdmin):
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": admin.ErrLastSuperAdmin.Error()})
	default:
		s.log.ErrorContext(c.UserContext(), "admin operation failed", "error", err.Error())
		return internalError(c)
	}
}

// parseUUIDParam reads and validates a uuid path parameter.
func parseUUIDParam(c *fiber.Ctx, name string) (pgtype.UUID, bool) {
	id, err := parseUUID(c.Params(name))
	if err != nil {
		return pgtype.UUID{}, false
	}
	return id, true
}
