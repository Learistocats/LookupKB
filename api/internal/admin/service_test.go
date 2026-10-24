package admin

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

// fakeDB is an in-memory Queries. The tx runner snapshots it before fn and
// restores the snapshot when fn fails, so tests observe real rollback semantics.
type fakeDB struct {
	groups   map[pgtype.UUID]dbgen.Group
	members  map[[2]pgtype.UUID]bool // {group, user}
	grants   map[[2]pgtype.UUID]bool // {group, tag}
	tags     map[pgtype.UUID]dbgen.Tag
	users    map[pgtype.UUID]dbgen.User
	audits   []dbgen.InsertAuditLogParams
	auditErr error
}

func newFakeDB() *fakeDB {
	return &fakeDB{
		groups:  map[pgtype.UUID]dbgen.Group{},
		members: map[[2]pgtype.UUID]bool{},
		grants:  map[[2]pgtype.UUID]bool{},
		tags:    map[pgtype.UUID]dbgen.Tag{},
		users:   map[pgtype.UUID]dbgen.User{},
	}
}

func (f *fakeDB) snapshot() *fakeDB {
	return &fakeDB{
		groups: maps.Clone(f.groups), members: maps.Clone(f.members), grants: maps.Clone(f.grants),
		tags: maps.Clone(f.tags), users: maps.Clone(f.users), audits: slices.Clone(f.audits), auditErr: f.auditErr,
	}
}

func (f *fakeDB) restore(s *fakeDB) {
	f.groups, f.members, f.grants, f.tags, f.users, f.audits = s.groups, s.members, s.grants, s.tags, s.users, s.audits
}

func (f *fakeDB) service() *Service {
	return NewWithQueries(f, func(_ context.Context, fn func(q Queries) error) error {
		snap := f.snapshot()
		if err := fn(f); err != nil {
			f.restore(snap)
			return err
		}
		return nil
	})
}

func newID() pgtype.UUID { return pgtype.UUID{Bytes: uuid.New(), Valid: true} }

func (f *fakeDB) addUser(email string, super bool) pgtype.UUID {
	id := newID()
	f.users[id] = dbgen.User{ID: id, Email: email, IsSuperAdmin: super}
	return id
}

func (f *fakeDB) addGroup(name string) pgtype.UUID {
	id := newID()
	f.groups[id] = dbgen.Group{ID: id, Name: name}
	return id
}

func (f *fakeDB) addTag(slug string) pgtype.UUID {
	id := newID()
	f.tags[id] = dbgen.Tag{ID: id, Slug: slug, Label: slug}
	return id
}

func (f *fakeDB) actions() []string {
	out := make([]string, len(f.audits))
	for i, a := range f.audits {
		out[i] = a.Action
	}
	return out
}

// --- Queries implementation ---

func (f *fakeDB) CreateGroup(_ context.Context, name string) (dbgen.Group, error) {
	for _, g := range f.groups {
		if g.Name == name {
			return dbgen.Group{}, &pgconn.PgError{Code: "23505"}
		}
	}
	id := newID()
	f.groups[id] = dbgen.Group{ID: id, Name: name}
	return f.groups[id], nil
}

func (f *fakeDB) RenameGroup(_ context.Context, arg dbgen.RenameGroupParams) (dbgen.Group, error) {
	g, ok := f.groups[arg.ID]
	if !ok {
		return dbgen.Group{}, pgx.ErrNoRows
	}
	for id, o := range f.groups {
		if id != arg.ID && o.Name == arg.Name {
			return dbgen.Group{}, &pgconn.PgError{Code: "23505"}
		}
	}
	g.Name = arg.Name
	f.groups[arg.ID] = g
	return g, nil
}

func (f *fakeDB) DeleteGroup(_ context.Context, id pgtype.UUID) (int64, error) {
	if _, ok := f.groups[id]; !ok {
		return 0, nil
	}
	delete(f.groups, id)
	return 1, nil
}

func (f *fakeDB) GetGroup(_ context.Context, id pgtype.UUID) (dbgen.Group, error) {
	g, ok := f.groups[id]
	if !ok {
		return dbgen.Group{}, pgx.ErrNoRows
	}
	return g, nil
}

func (f *fakeDB) ListGroups(context.Context) ([]dbgen.ListGroupsRow, error) {
	var out []dbgen.ListGroupsRow
	for _, g := range f.groups {
		out = append(out, dbgen.ListGroupsRow{ID: g.ID, Name: g.Name})
	}
	return out, nil
}

func (f *fakeDB) ListGroupMembers(_ context.Context, gid pgtype.UUID) ([]dbgen.ListGroupMembersRow, error) {
	var out []dbgen.ListGroupMembersRow
	for k := range f.members {
		if k[0] == gid {
			u := f.users[k[1]]
			out = append(out, dbgen.ListGroupMembersRow{ID: u.ID, Email: u.Email})
		}
	}
	return out, nil
}

func (f *fakeDB) AddGroupMember(_ context.Context, arg dbgen.AddGroupMemberParams) (int64, error) {
	k := [2]pgtype.UUID{arg.GroupID, arg.UserID}
	if f.members[k] {
		return 0, nil
	}
	f.members[k] = true
	return 1, nil
}

func (f *fakeDB) RemoveGroupMember(_ context.Context, arg dbgen.RemoveGroupMemberParams) (int64, error) {
	k := [2]pgtype.UUID{arg.GroupID, arg.UserID}
	if !f.members[k] {
		return 0, nil
	}
	delete(f.members, k)
	return 1, nil
}

func (f *fakeDB) ListGroupTagGrants(_ context.Context, gid pgtype.UUID) ([]dbgen.ListGroupTagGrantsRow, error) {
	var out []dbgen.ListGroupTagGrantsRow
	for k := range f.grants {
		if k[0] == gid {
			t := f.tags[k[1]]
			out = append(out, dbgen.ListGroupTagGrantsRow{TagID: t.ID, Slug: t.Slug, Label: t.Label})
		}
	}
	return out, nil
}

func (f *fakeDB) AddGroupTagGrant(_ context.Context, arg dbgen.AddGroupTagGrantParams) (int64, error) {
	k := [2]pgtype.UUID{arg.GroupID, arg.TagID}
	if f.grants[k] {
		return 0, nil
	}
	f.grants[k] = true
	return 1, nil
}

func (f *fakeDB) RemoveGroupTagGrant(_ context.Context, arg dbgen.RemoveGroupTagGrantParams) (int64, error) {
	k := [2]pgtype.UUID{arg.GroupID, arg.TagID}
	if !f.grants[k] {
		return 0, nil
	}
	delete(f.grants, k)
	return 1, nil
}

func (f *fakeDB) CountTagGrants(_ context.Context, tagID pgtype.UUID) (int64, error) {
	var n int64
	for k := range f.grants {
		if k[1] == tagID {
			n++
		}
	}
	return n, nil
}

func (f *fakeDB) UpsertTag(_ context.Context, arg dbgen.UpsertTagParams) (dbgen.Tag, error) {
	for _, t := range f.tags {
		if t.Slug == arg.Slug {
			return t, nil
		}
	}
	id := newID()
	f.tags[id] = dbgen.Tag{ID: id, Slug: arg.Slug, Label: arg.Label}
	return f.tags[id], nil
}

func (f *fakeDB) GetTagByID(_ context.Context, id pgtype.UUID) (dbgen.Tag, error) {
	t, ok := f.tags[id]
	if !ok {
		return dbgen.Tag{}, pgx.ErrNoRows
	}
	return t, nil
}

func (f *fakeDB) GetSettings(context.Context, []string) ([]dbgen.GetSettingsRow, error) {
	return nil, nil
}

func (f *fakeDB) GetUserByEmail(_ context.Context, email string) (dbgen.User, error) {
	for _, u := range f.users {
		if strings.EqualFold(u.Email, email) {
			return u, nil
		}
	}
	return dbgen.User{}, pgx.ErrNoRows
}

func (f *fakeDB) ListUsers(context.Context) ([]dbgen.ListUsersRow, error) {
	var out []dbgen.ListUsersRow
	for _, u := range f.users {
		out = append(out, dbgen.ListUsersRow{ID: u.ID, Email: u.Email, IsSuperAdmin: u.IsSuperAdmin})
	}
	return out, nil
}

func (f *fakeDB) SetUserSuperAdmin(_ context.Context, arg dbgen.SetUserSuperAdminParams) (dbgen.SetUserSuperAdminRow, error) {
	u, ok := f.users[arg.ID]
	if !ok {
		return dbgen.SetUserSuperAdminRow{}, pgx.ErrNoRows
	}
	u.IsSuperAdmin = arg.IsSuperAdmin
	f.users[arg.ID] = u
	return dbgen.SetUserSuperAdminRow{ID: u.ID, Email: u.Email, IsSuperAdmin: u.IsSuperAdmin}, nil
}

func (f *fakeDB) SetUserSuperAdminByEmail(ctx context.Context, arg dbgen.SetUserSuperAdminByEmailParams) (dbgen.SetUserSuperAdminByEmailRow, error) {
	u, err := f.GetUserByEmail(ctx, arg.Email)
	if err != nil {
		return dbgen.SetUserSuperAdminByEmailRow{}, err
	}
	u.IsSuperAdmin = arg.IsSuperAdmin
	f.users[u.ID] = u
	return dbgen.SetUserSuperAdminByEmailRow{ID: u.ID, Email: u.Email, IsSuperAdmin: u.IsSuperAdmin}, nil
}

func (f *fakeDB) LockSuperAdmins(context.Context) ([]pgtype.UUID, error) {
	var out []pgtype.UUID
	for id, u := range f.users {
		if u.IsSuperAdmin {
			out = append(out, id)
		}
	}
	return out, nil
}

func (f *fakeDB) InsertAuditLog(_ context.Context, arg dbgen.InsertAuditLogParams) error {
	if f.auditErr != nil {
		return f.auditErr
	}
	f.audits = append(f.audits, arg)
	return nil
}

// --- tests ---

var ctx = context.Background()

func TestCreateGroup_ValidatesTrimsAndAudits(t *testing.T) {
	f := newFakeDB()
	actor := f.addUser("admin@example.com", true)
	s := f.service()

	g, err := s.CreateGroup(ctx, actor, "  Ops team  ")
	require.NoError(t, err)
	require.Equal(t, "Ops team", g.Name)
	require.Equal(t, []string{ActionGroupCreate}, f.actions())
	require.Equal(t, actor, f.audits[0].ActorID)
	require.Equal(t, "group", f.audits[0].TargetType)

	var verr *ValidationError
	_, err = s.CreateGroup(ctx, actor, "   ")
	require.ErrorAs(t, err, &verr)
	_, err = s.CreateGroup(ctx, actor, strings.Repeat("x", MaxGroupNameRunes+1))
	require.ErrorAs(t, err, &verr)
	_, err = s.CreateGroup(ctx, actor, "bad\x00name")
	require.ErrorAs(t, err, &verr)

	_, err = s.CreateGroup(ctx, actor, "Ops team")
	require.ErrorIs(t, err, ErrGroupNameTaken)
	require.Len(t, f.audits, 1, "failed creates write no audit row")
}

func TestRenameAndDeleteGroup(t *testing.T) {
	f := newFakeDB()
	actor := f.addUser("admin@example.com", true)
	gid := f.addGroup("ops")
	s := f.service()

	g, err := s.RenameGroup(ctx, actor, gid, "sre")
	require.NoError(t, err)
	require.Equal(t, "sre", g.Name)

	_, err = s.RenameGroup(ctx, actor, newID(), "x")
	require.ErrorIs(t, err, ErrNotFound)

	require.NoError(t, s.DeleteGroup(ctx, actor, gid))
	require.ErrorIs(t, s.DeleteGroup(ctx, actor, gid), ErrNotFound)
	require.Equal(t, []string{ActionGroupRename, ActionGroupDelete}, f.actions())
}

func TestMembers_AddByEmailRemoveAndIdempotent(t *testing.T) {
	f := newFakeDB()
	actor := f.addUser("admin@example.com", true)
	uid := f.addUser("Ada@Example.com", false)
	gid := f.addGroup("ops")
	s := f.service()

	u, err := s.AddMember(ctx, actor, gid, " ada@example.com ")
	require.NoError(t, err)
	require.Equal(t, uid, u.ID)
	_, err = s.AddMember(ctx, actor, gid, "ada@example.com")
	require.NoError(t, err, "re-adding is a no-op")
	require.Equal(t, []string{ActionGroupMemberAdd}, f.actions(), "no duplicate audit")

	_, err = s.AddMember(ctx, actor, gid, "nobody@example.com")
	require.ErrorIs(t, err, ErrNotFound)
	_, err = s.AddMember(ctx, actor, newID(), "ada@example.com")
	require.ErrorIs(t, err, ErrNotFound)

	require.NoError(t, s.RemoveMember(ctx, actor, gid, uid))
	require.ErrorIs(t, s.RemoveMember(ctx, actor, gid, uid), ErrNotFound)
	require.Equal(t, []string{ActionGroupMemberAdd, ActionGroupMemberRemove}, f.actions())
}

func TestGrantTag_FirstGrantNeedsConfirmation(t *testing.T) {
	f := newFakeDB()
	actor := f.addUser("admin@example.com", true)
	g1 := f.addGroup("ops")
	g2 := f.addGroup("sre")
	s := f.service()

	// Unconfirmed first grant: 409-style error, and nothing is written (not even
	// the new tag row — the upsert rolls back with the tx).
	_, err := s.GrantTag(ctx, actor, g1, "  Secret Stuff ", false)
	var cerr *NeedsConfirmationError
	require.ErrorAs(t, err, &cerr)
	require.Equal(t, "secret-stuff", cerr.Tag)
	require.Empty(t, f.grants)
	require.Empty(t, f.tags)
	require.Empty(t, f.audits)

	tag, err := s.GrantTag(ctx, actor, g1, "Secret Stuff", true)
	require.NoError(t, err)
	require.Equal(t, "secret-stuff", tag.Slug)
	require.Len(t, f.grants, 1)

	// Already protected: a second group needs no confirmation.
	_, err = s.GrantTag(ctx, actor, g2, "secret-stuff", false)
	require.NoError(t, err)
	require.Len(t, f.grants, 2)

	// Re-granting the same pair is idempotent (no extra audit).
	_, err = s.GrantTag(ctx, actor, g2, "secret-stuff", false)
	require.NoError(t, err)
	require.Equal(t, []string{ActionGroupTagGrant, ActionGroupTagGrant}, f.actions())

	var meta map[string]any
	require.NoError(t, json.Unmarshal(f.audits[0].Metadata, &meta))
	require.Equal(t, true, meta["first_grant"])
}

func TestGrantTag_RejectsInvalidAndUnknownGroup(t *testing.T) {
	f := newFakeDB()
	actor := f.addUser("admin@example.com", true)
	s := f.service()

	var verr *ValidationError
	_, err := s.GrantTag(ctx, actor, f.addGroup("ops"), "  !! ", true)
	require.ErrorAs(t, err, &verr)
	_, err = s.GrantTag(ctx, actor, f.addGroup("sre"), "x", true) // below MinLen
	require.ErrorAs(t, err, &verr)
	_, err = s.GrantTag(ctx, actor, newID(), "valid", true)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestRevokeTag_LastGrantNeedsConfirmation(t *testing.T) {
	f := newFakeDB()
	actor := f.addUser("admin@example.com", true)
	g1 := f.addGroup("ops")
	g2 := f.addGroup("sre")
	tag := f.addTag("secret")
	f.grants[[2]pgtype.UUID{g1, tag}] = true
	f.grants[[2]pgtype.UUID{g2, tag}] = true
	s := f.service()

	// Not the last grant: no confirmation.
	require.NoError(t, s.RevokeTag(ctx, actor, g1, tag, false))

	// The last grant: refused unconfirmed, and the delete is rolled back.
	err := s.RevokeTag(ctx, actor, g2, tag, false)
	var cerr *NeedsConfirmationError
	require.ErrorAs(t, err, &cerr)
	require.Equal(t, "secret", cerr.Tag)
	require.True(t, f.grants[[2]pgtype.UUID{g2, tag}])

	require.NoError(t, s.RevokeTag(ctx, actor, g2, tag, true))
	require.Empty(t, f.grants)
	require.Equal(t, []string{ActionGroupTagRevoke, ActionGroupTagRevoke}, f.actions())

	require.ErrorIs(t, s.RevokeTag(ctx, actor, g2, tag, true), ErrNotFound, "not granted")
	require.ErrorIs(t, s.RevokeTag(ctx, actor, g2, newID(), true), ErrNotFound, "unknown tag")
}

func TestSetSuperAdmin_LastAdminGuard(t *testing.T) {
	f := newFakeDB()
	a := f.addUser("a@example.com", true)
	b := f.addUser("b@example.com", false)
	s := f.service()

	// The only admin cannot demote themselves.
	_, err := s.SetSuperAdmin(ctx, a, a, false)
	require.ErrorIs(t, err, ErrLastSuperAdmin)
	require.True(t, f.users[a].IsSuperAdmin)

	row, err := s.SetSuperAdmin(ctx, a, b, true)
	require.NoError(t, err)
	require.True(t, row.IsSuperAdmin)

	// With two admins, one may be demoted; then the other is the last again.
	_, err = s.SetSuperAdmin(ctx, b, a, false)
	require.NoError(t, err)
	_, err = s.SetSuperAdmin(ctx, b, b, false)
	require.ErrorIs(t, err, ErrLastSuperAdmin)

	// Demoting a non-admin is allowed even when only one admin exists.
	c := f.addUser("c@example.com", false)
	_, err = s.SetSuperAdmin(ctx, b, c, false)
	require.NoError(t, err)

	_, err = s.SetSuperAdmin(ctx, b, newID(), true)
	require.ErrorIs(t, err, ErrNotFound)

	require.Equal(t, []string{ActionUserSuperAdminSet, ActionUserSuperAdminSet, ActionUserSuperAdminSet}, f.actions())
	require.Equal(t, a, f.audits[0].ActorID)
}

func TestSetSuperAdminByEmail_CLIAuditsWithNullActor(t *testing.T) {
	f := newFakeDB()
	uid := f.addUser("ops@example.com", false)
	s := f.service()

	row, err := s.SetSuperAdminByEmail(ctx, "OPS@example.com", true)
	require.NoError(t, err)
	require.Equal(t, uid, row.ID)
	require.True(t, f.users[uid].IsSuperAdmin)
	require.Len(t, f.audits, 1)
	require.False(t, f.audits[0].ActorID.Valid, "CLI actions have a null actor")

	_, err = s.SetSuperAdminByEmail(ctx, "nobody@example.com", true)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestMutation_AuditFailureRollsBack(t *testing.T) {
	f := newFakeDB()
	actor := f.addUser("admin@example.com", true)
	f.auditErr = errors.New("audit down")
	s := f.service()

	_, err := s.CreateGroup(ctx, actor, "ops")
	require.Error(t, err)
	require.Empty(t, f.groups, "the mutation rolls back with its audit row")
}
