// Package admin is the super-admin management surface (LOO-56, F10): local groups,
// their membership, their authorized tag policy (group_tag_grants) and the
// super-admin role itself.
//
// Every mutation runs in ONE transaction together with its audit_log row, so an
// audited fact can never be lost and a failed audit write rolls the change back.
// The HTTP layer (internal/server/admin.go) only maps inputs/errors; all rules live
// here so they are unit-testable with a fake Queries (no database).
//
// Protected-tag ENFORCEMENT (who may read/publish an entry carrying a granted tag)
// is LOO-33; this package only maintains the policy.
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/Learistocats/LookupKB/api/internal/tags"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MaxGroupNameRunes bounds a group name (after trimming).
const MaxGroupNameRunes = 100

// Audit action names (03-data-model.md §1 audit_log).
const (
	ActionGroupCreate       = "group.create"
	ActionGroupRename       = "group.rename"
	ActionGroupDelete       = "group.delete"
	ActionGroupMemberAdd    = "group.member_add"
	ActionGroupMemberRemove = "group.member_remove"
	ActionGroupTagGrant     = "group.tag_grant"
	ActionGroupTagRevoke    = "group.tag_revoke"
	ActionUserSuperAdminSet = "user.super_admin_set"
)

var (
	// ErrNotFound means the group, user, tag, membership or grant does not exist.
	ErrNotFound = errors.New("not found")
	// ErrGroupNameTaken means another group already has this name (UNIQUE).
	ErrGroupNameTaken = errors.New("a group with this name already exists")
	// ErrLastSuperAdmin means the change would leave the instance with no super-admin.
	ErrLastSuperAdmin = errors.New("cannot remove the last super-admin")
)

// ValidationError is a client input problem (bad group name, empty tag, ...).
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

// NeedsConfirmationError is returned when a tag-policy change flips a tag's
// visibility class — the first grant makes a public tag protected, removing the
// last grant makes it public again — and the caller did not confirm. Nothing is
// written; the caller retries with the confirm flag set.
type NeedsConfirmationError struct {
	// Tag is the tag's label, echoed so the UI can phrase the prompt.
	Tag string
}

func (e *NeedsConfirmationError) Error() string {
	return fmt.Sprintf("changing the protection of tag %q needs confirmation", e.Tag)
}

// Queries is the narrow query surface the service needs. *dbgen.Queries satisfies
// it (directly and via WithTx); tests use an in-memory fake.
type Queries interface {
	// groups
	CreateGroup(ctx context.Context, name string) (dbgen.Group, error)
	RenameGroup(ctx context.Context, arg dbgen.RenameGroupParams) (dbgen.Group, error)
	DeleteGroup(ctx context.Context, id pgtype.UUID) (int64, error)
	GetGroup(ctx context.Context, id pgtype.UUID) (dbgen.Group, error)
	ListGroups(ctx context.Context) ([]dbgen.ListGroupsRow, error)
	ListGroupMembers(ctx context.Context, groupID pgtype.UUID) ([]dbgen.ListGroupMembersRow, error)
	AddGroupMember(ctx context.Context, arg dbgen.AddGroupMemberParams) (int64, error)
	RemoveGroupMember(ctx context.Context, arg dbgen.RemoveGroupMemberParams) (int64, error)
	// tag policy
	ListGroupTagGrants(ctx context.Context, groupID pgtype.UUID) ([]dbgen.ListGroupTagGrantsRow, error)
	AddGroupTagGrant(ctx context.Context, arg dbgen.AddGroupTagGrantParams) (int64, error)
	RemoveGroupTagGrant(ctx context.Context, arg dbgen.RemoveGroupTagGrantParams) (int64, error)
	CountTagGrants(ctx context.Context, tagID pgtype.UUID) (int64, error)
	UpsertTag(ctx context.Context, arg dbgen.UpsertTagParams) (dbgen.Tag, error)
	GetTagByID(ctx context.Context, id pgtype.UUID) (dbgen.Tag, error)
	GetSetting(ctx context.Context, key string) ([]byte, error)
	// users
	GetUserByEmail(ctx context.Context, email string) (dbgen.User, error)
	ListUsers(ctx context.Context) ([]dbgen.ListUsersRow, error)
	SetUserSuperAdmin(ctx context.Context, arg dbgen.SetUserSuperAdminParams) (dbgen.SetUserSuperAdminRow, error)
	SetUserSuperAdminByEmail(ctx context.Context, arg dbgen.SetUserSuperAdminByEmailParams) (dbgen.SetUserSuperAdminByEmailRow, error)
	LockSuperAdmins(ctx context.Context) ([]pgtype.UUID, error)
	// audit
	InsertAuditLog(ctx context.Context, arg dbgen.InsertAuditLogParams) error
}

// TxFunc runs fn inside one transaction: commit when fn returns nil, roll back
// otherwise.
type TxFunc func(ctx context.Context, fn func(q Queries) error) error

// Service implements the admin rules.
type Service struct {
	q    Queries
	inTx TxFunc
}

// New builds a Service over the pool: reads go straight to the pool, mutations
// run in a pool transaction.
func New(pool *pgxpool.Pool) *Service {
	base := dbgen.New(pool)
	return NewWithQueries(base, func(ctx context.Context, fn func(q Queries) error) error {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin admin tx: %w", err)
		}
		defer tx.Rollback(ctx) //nolint:errcheck // no-op after Commit; safe rollback on any early return
		if err := fn(base.WithTx(tx)); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit admin tx: %w", err)
		}
		return nil
	})
}

// NewWithQueries builds a Service over an arbitrary Queries + transaction runner
// (tests pass a fake and a runner that calls fn directly).
func NewWithQueries(q Queries, inTx TxFunc) *Service {
	return &Service{q: q, inTx: inTx}
}

// GroupDetail is one group with its members and tag grants.
type GroupDetail struct {
	Group   dbgen.Group
	Members []dbgen.ListGroupMembersRow
	Tags    []dbgen.ListGroupTagGrantsRow
}

// ListGroups returns every group with its member/tag counts.
func (s *Service) ListGroups(ctx context.Context) ([]dbgen.ListGroupsRow, error) {
	return s.q.ListGroups(ctx)
}

// GetGroup returns a group with its members and tag grants.
func (s *Service) GetGroup(ctx context.Context, id pgtype.UUID) (GroupDetail, error) {
	g, err := s.q.GetGroup(ctx, id)
	if err != nil {
		return GroupDetail{}, notFoundOr(err, "get group")
	}
	members, err := s.q.ListGroupMembers(ctx, id)
	if err != nil {
		return GroupDetail{}, fmt.Errorf("list members: %w", err)
	}
	grants, err := s.q.ListGroupTagGrants(ctx, id)
	if err != nil {
		return GroupDetail{}, fmt.Errorf("list tag grants: %w", err)
	}
	return GroupDetail{Group: g, Members: members, Tags: grants}, nil
}

// CreateGroup creates a named group.
func (s *Service) CreateGroup(ctx context.Context, actor pgtype.UUID, rawName string) (dbgen.Group, error) {
	name, err := ValidateGroupName(rawName)
	if err != nil {
		return dbgen.Group{}, err
	}
	var out dbgen.Group
	err = s.inTx(ctx, func(q Queries) error {
		g, err := q.CreateGroup(ctx, name)
		if err != nil {
			return groupWriteErr(err, "create group")
		}
		out = g
		return audit(ctx, q, actor, ActionGroupCreate, "group", formatUUID(g.ID), map[string]any{"name": name})
	})
	return out, err
}

// RenameGroup renames a group.
func (s *Service) RenameGroup(ctx context.Context, actor, id pgtype.UUID, rawName string) (dbgen.Group, error) {
	name, err := ValidateGroupName(rawName)
	if err != nil {
		return dbgen.Group{}, err
	}
	var out dbgen.Group
	err = s.inTx(ctx, func(q Queries) error {
		before, err := q.GetGroup(ctx, id)
		if err != nil {
			return notFoundOr(err, "get group")
		}
		g, err := q.RenameGroup(ctx, dbgen.RenameGroupParams{ID: id, Name: name})
		if err != nil {
			return groupWriteErr(err, "rename group")
		}
		out = g
		return audit(ctx, q, actor, ActionGroupRename, "group", formatUUID(id), map[string]any{"from": before.Name, "to": name})
	})
	return out, err
}

// DeleteGroup deletes a group (its memberships and tag grants cascade).
func (s *Service) DeleteGroup(ctx context.Context, actor, id pgtype.UUID) error {
	return s.inTx(ctx, func(q Queries) error {
		g, err := q.GetGroup(ctx, id)
		if err != nil {
			return notFoundOr(err, "get group")
		}
		n, err := q.DeleteGroup(ctx, id)
		if err != nil {
			return fmt.Errorf("delete group: %w", err)
		}
		if n == 0 {
			return ErrNotFound
		}
		return audit(ctx, q, actor, ActionGroupDelete, "group", formatUUID(id), map[string]any{"name": g.Name})
	})
}

// AddMember adds the user with this email to the group. Re-adding an existing
// member is a no-op (no audit row). Unknown email or group → ErrNotFound.
func (s *Service) AddMember(ctx context.Context, actor, groupID pgtype.UUID, email string) (dbgen.User, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return dbgen.User{}, &ValidationError{Message: "email is required"}
	}
	var out dbgen.User
	err := s.inTx(ctx, func(q Queries) error {
		if _, err := q.GetGroup(ctx, groupID); err != nil {
			return notFoundOr(err, "get group")
		}
		u, err := q.GetUserByEmail(ctx, email)
		if err != nil {
			return notFoundOr(err, "get user")
		}
		out = u
		n, err := q.AddGroupMember(ctx, dbgen.AddGroupMemberParams{GroupID: groupID, UserID: u.ID})
		if err != nil {
			return fmt.Errorf("add member: %w", err)
		}
		if n == 0 {
			return nil // already a member
		}
		return audit(ctx, q, actor, ActionGroupMemberAdd, "group", formatUUID(groupID), map[string]any{"user_id": formatUUID(u.ID)})
	})
	return out, err
}

// RemoveMember removes a user from the group; not a member → ErrNotFound.
func (s *Service) RemoveMember(ctx context.Context, actor, groupID, userID pgtype.UUID) error {
	return s.inTx(ctx, func(q Queries) error {
		n, err := q.RemoveGroupMember(ctx, dbgen.RemoveGroupMemberParams{GroupID: groupID, UserID: userID})
		if err != nil {
			return fmt.Errorf("remove member: %w", err)
		}
		if n == 0 {
			return ErrNotFound
		}
		return audit(ctx, q, actor, ActionGroupMemberRemove, "group", formatUUID(groupID), map[string]any{"user_id": formatUUID(userID)})
	})
}

// GrantTag adds a tag to the group's authorized tag policy. The raw tag is
// normalized with the single tag rule (internal/tags) and created if new. When the
// tag currently has NO grant, this grant makes it protected: without confirm the
// call returns *NeedsConfirmationError and writes nothing.
func (s *Service) GrantTag(ctx context.Context, actor, groupID pgtype.UUID, rawTag string, confirm bool) (dbgen.Tag, error) {
	slug := tags.Normalize(rawTag)
	if slug == "" {
		return dbgen.Tag{}, &ValidationError{Message: "tag is required"}
	}
	var out dbgen.Tag
	err := s.inTx(ctx, func(q Queries) error {
		limits, err := tags.LoadLimits(ctx, q)
		if err != nil {
			return fmt.Errorf("load tag limits: %w", err)
		}
		if n := utf8.RuneCountInString(slug); n < limits.MinLen || n > limits.MaxLen {
			return &ValidationError{Message: fmt.Sprintf("tag must be %d-%d characters", limits.MinLen, limits.MaxLen)}
		}
		if _, err := q.GetGroup(ctx, groupID); err != nil {
			return notFoundOr(err, "get group")
		}
		tag, err := q.UpsertTag(ctx, dbgen.UpsertTagParams{Slug: slug, Label: slug})
		if err != nil {
			return fmt.Errorf("upsert tag: %w", err)
		}
		out = tag
		count, err := q.CountTagGrants(ctx, tag.ID)
		if err != nil {
			return fmt.Errorf("count tag grants: %w", err)
		}
		if count == 0 && !confirm {
			// Rolls back the upsert too: an unconfirmed attempt leaves no trace.
			return &NeedsConfirmationError{Tag: tag.Label}
		}
		n, err := q.AddGroupTagGrant(ctx, dbgen.AddGroupTagGrantParams{GroupID: groupID, TagID: tag.ID})
		if err != nil {
			return fmt.Errorf("add tag grant: %w", err)
		}
		if n == 0 {
			return nil // already granted to this group
		}
		return audit(ctx, q, actor, ActionGroupTagGrant, "group", formatUUID(groupID), map[string]any{
			"tag_id":      formatUUID(tag.ID),
			"tag":         tag.Slug,
			"first_grant": count == 0,
		})
	})
	return out, err
}

// RevokeTag removes a tag from the group's policy. When it is the tag's LAST
// grant the tag becomes public again: without confirm the call returns
// *NeedsConfirmationError and writes nothing. Not granted → ErrNotFound.
func (s *Service) RevokeTag(ctx context.Context, actor, groupID, tagID pgtype.UUID, confirm bool) error {
	return s.inTx(ctx, func(q Queries) error {
		tag, err := q.GetTagByID(ctx, tagID)
		if err != nil {
			return notFoundOr(err, "get tag")
		}
		count, err := q.CountTagGrants(ctx, tagID)
		if err != nil {
			return fmt.Errorf("count tag grants: %w", err)
		}
		n, err := q.RemoveGroupTagGrant(ctx, dbgen.RemoveGroupTagGrantParams{GroupID: groupID, TagID: tagID})
		if err != nil {
			return fmt.Errorf("remove tag grant: %w", err)
		}
		if n == 0 {
			return ErrNotFound
		}
		if count <= 1 && !confirm {
			return &NeedsConfirmationError{Tag: tag.Label} // rolls the delete back
		}
		return audit(ctx, q, actor, ActionGroupTagRevoke, "group", formatUUID(groupID), map[string]any{
			"tag_id":     formatUUID(tagID),
			"tag":        tag.Slug,
			"last_grant": count <= 1,
		})
	})
}

// ListUsers returns every account (never the password hash).
func (s *Service) ListUsers(ctx context.Context) ([]dbgen.ListUsersRow, error) {
	return s.q.ListUsers(ctx)
}

// SetSuperAdmin grants or revokes the super-admin role. Revoking the last
// super-admin is refused with ErrLastSuperAdmin (the instance would have no one
// able to administer it through the API). The super-admin rows are locked first,
// so concurrent demotions cannot race past the guard.
func (s *Service) SetSuperAdmin(ctx context.Context, actor, userID pgtype.UUID, value bool) (dbgen.SetUserSuperAdminRow, error) {
	var out dbgen.SetUserSuperAdminRow
	err := s.inTx(ctx, func(q Queries) error {
		if !value {
			admins, err := q.LockSuperAdmins(ctx)
			if err != nil {
				return fmt.Errorf("lock super-admins: %w", err)
			}
			if len(admins) == 1 && admins[0] == userID {
				return ErrLastSuperAdmin
			}
		}
		row, err := q.SetUserSuperAdmin(ctx, dbgen.SetUserSuperAdminParams{ID: userID, IsSuperAdmin: value})
		if err != nil {
			return notFoundOr(err, "set super-admin")
		}
		out = row
		return audit(ctx, q, actor, ActionUserSuperAdminSet, "user", formatUUID(userID), map[string]any{"value": value})
	})
	return out, err
}

// SetSuperAdminByEmail is the CLI bootstrap (`api admin grant|revoke <email>`).
// The audit row has a null actor (system action). It deliberately skips the
// last-super-admin guard: the operator holding the binary can always re-grant, so
// there is no lock-out risk, and it is the break-glass path.
func (s *Service) SetSuperAdminByEmail(ctx context.Context, email string, value bool) (dbgen.SetUserSuperAdminByEmailRow, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return dbgen.SetUserSuperAdminByEmailRow{}, &ValidationError{Message: "email is required"}
	}
	var out dbgen.SetUserSuperAdminByEmailRow
	err := s.inTx(ctx, func(q Queries) error {
		row, err := q.SetUserSuperAdminByEmail(ctx, dbgen.SetUserSuperAdminByEmailParams{Email: email, IsSuperAdmin: value})
		if err != nil {
			return notFoundOr(err, "set super-admin")
		}
		out = row
		return audit(ctx, q, pgtype.UUID{}, ActionUserSuperAdminSet, "user", formatUUID(row.ID), map[string]any{
			"value":  value,
			"source": "cli",
		})
	})
	return out, err
}

// ValidateGroupName trims and bounds a group name: 1..MaxGroupNameRunes runes, no
// control characters.
func ValidateGroupName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", &ValidationError{Message: "group name is required"}
	}
	if utf8.RuneCountInString(name) > MaxGroupNameRunes {
		return "", &ValidationError{Message: fmt.Sprintf("group name must be at most %d characters", MaxGroupNameRunes)}
	}
	if !utf8.ValidString(name) || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", &ValidationError{Message: "group name contains invalid characters"}
	}
	return name, nil
}

// audit writes one audit_log row on the caller's transaction.
func audit(ctx context.Context, q Queries, actor pgtype.UUID, action, targetType, targetID string, meta map[string]any) error {
	raw, err := json.Marshal(meta)
	if err != nil {
		raw = []byte("{}")
	}
	if err := q.InsertAuditLog(ctx, dbgen.InsertAuditLogParams{
		ActorID:    actor,
		Action:     action,
		TargetType: targetType,
		TargetID:   &targetID,
		Metadata:   raw,
	}); err != nil {
		return fmt.Errorf("write audit log: %w", err)
	}
	return nil
}

func notFoundOr(err error, what string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return fmt.Errorf("%s: %w", what, err)
}

func groupWriteErr(err error, what string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrGroupNameTaken
	}
	return notFoundOr(err, what)
}

func formatUUID(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return uuid.UUID(u.Bytes).String()
}
