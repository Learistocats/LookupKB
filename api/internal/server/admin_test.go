package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/Learistocats/LookupKB/api/internal/admin"
	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/Learistocats/LookupKB/api/internal/config"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

// fakeDirectory backs the real auth middleware: no groups, and a configurable
// super-admin flag (the per-request DB re-check).
type fakeDirectory struct{ super bool }

func (f *fakeDirectory) ListUserGroupIDs(context.Context, pgtype.UUID) ([]pgtype.UUID, error) {
	return nil, nil
}

func (f *fakeDirectory) GetUserIsSuperAdmin(context.Context, pgtype.UUID) (bool, error) {
	return f.super, nil
}

// fakeAdmin records calls and returns canned errors; the rules themselves are
// unit-tested in internal/admin.
type fakeAdmin struct {
	calls      []string
	err        error
	actor      pgtype.UUID
	confirm    bool
	superValue bool
}

func (f *fakeAdmin) rec(name string) { f.calls = append(f.calls, name) }

func (f *fakeAdmin) ListGroups(context.Context) ([]dbgen.ListGroupsRow, error) {
	f.rec("ListGroups")
	return []dbgen.ListGroupsRow{{ID: pgUUID(uuid.New()), Name: "ops", MemberCount: 2, TagCount: 1}}, f.err
}

func (f *fakeAdmin) GetGroup(_ context.Context, id pgtype.UUID) (admin.GroupDetail, error) {
	f.rec("GetGroup")
	return admin.GroupDetail{Group: dbgen.Group{ID: id, Name: "ops"}}, f.err
}

func (f *fakeAdmin) CreateGroup(_ context.Context, actor pgtype.UUID, name string) (dbgen.Group, error) {
	f.rec("CreateGroup")
	f.actor = actor
	return dbgen.Group{ID: pgUUID(uuid.New()), Name: name}, f.err
}

func (f *fakeAdmin) RenameGroup(_ context.Context, _, id pgtype.UUID, name string) (dbgen.Group, error) {
	f.rec("RenameGroup")
	return dbgen.Group{ID: id, Name: name}, f.err
}

func (f *fakeAdmin) DeleteGroup(context.Context, pgtype.UUID, pgtype.UUID) error {
	f.rec("DeleteGroup")
	return f.err
}

func (f *fakeAdmin) AddMember(context.Context, pgtype.UUID, pgtype.UUID, string) (dbgen.User, error) {
	f.rec("AddMember")
	return dbgen.User{ID: pgUUID(uuid.New()), Email: "ada@example.com", PasswordHash: "secret-hash"}, f.err
}

func (f *fakeAdmin) RemoveMember(context.Context, pgtype.UUID, pgtype.UUID, pgtype.UUID) error {
	f.rec("RemoveMember")
	return f.err
}

func (f *fakeAdmin) GrantTag(_ context.Context, _, _ pgtype.UUID, tag string, confirm bool) (dbgen.Tag, error) {
	f.rec("GrantTag")
	f.confirm = confirm
	return dbgen.Tag{ID: pgUUID(uuid.New()), Slug: tag, Label: tag}, f.err
}

func (f *fakeAdmin) RevokeTag(_ context.Context, _, _, _ pgtype.UUID, confirm bool) error {
	f.rec("RevokeTag")
	f.confirm = confirm
	return f.err
}

func (f *fakeAdmin) ListUsers(context.Context) ([]dbgen.ListUsersRow, error) {
	f.rec("ListUsers")
	return []dbgen.ListUsersRow{{ID: pgUUID(uuid.New()), Email: "a@example.com", IsSuperAdmin: true}}, f.err
}

func (f *fakeAdmin) SetSuperAdmin(_ context.Context, _, id pgtype.UUID, value bool) (dbgen.SetUserSuperAdminRow, error) {
	f.rec("SetSuperAdmin")
	f.superValue = value
	return dbgen.SetUserSuperAdminRow{ID: id, IsSuperAdmin: value}, f.err
}

func newAdminServer(t *testing.T, super bool, svc AdminService) *Server {
	t.Helper()
	cfg, err := config.Load()
	require.NoError(t, err)
	mw := auth.NewMiddleware(testTokens, &fakeDirectory{super: super}, slogDiscard())
	opts := []Option{WithAuth(mw)}
	if svc != nil {
		opts = append(opts, WithAdmin(svc))
	}
	return New(cfg, slogDiscard(), "test", opts...)
}

func readJSON(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(raw, &out), string(raw))
	return out
}

func TestAdmin_NonAdminIs403EverywhereAndServiceUntouched(t *testing.T) {
	svc := &fakeAdmin{}
	s := newAdminServer(t, false, svc)
	gid := uuid.NewString()
	for _, r := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/admin/groups", ""},
		{http.MethodPost, "/api/admin/groups", `{"name":"x"}`},
		{http.MethodGet, "/api/admin/groups/" + gid, ""},
		{http.MethodPatch, "/api/admin/groups/" + gid, `{"name":"x"}`},
		{http.MethodDelete, "/api/admin/groups/" + gid, ""},
		{http.MethodPost, "/api/admin/groups/" + gid + "/members", `{"email":"a@b.c"}`},
		{http.MethodDelete, "/api/admin/groups/" + gid + "/members/" + uuid.NewString(), ""},
		{http.MethodPost, "/api/admin/groups/" + gid + "/tags", `{"tag":"x"}`},
		{http.MethodDelete, "/api/admin/groups/" + gid + "/tags/" + uuid.NewString(), ""},
		{http.MethodGet, "/api/admin/users", ""},
		{http.MethodPut, "/api/admin/users/" + uuid.NewString() + "/super-admin", `{"value":true}`},
	} {
		resp := doReq(t, s, r.method, r.path, r.body)
		require.Equal(t, http.StatusForbidden, resp.StatusCode, "%s %s", r.method, r.path)
		_ = resp.Body.Close()
	}
	require.Empty(t, svc.calls)
}

func TestAdmin_RoutesAbsentWithoutService(t *testing.T) {
	s := newAdminServer(t, true, nil)
	resp := doReq(t, s, http.MethodGet, "/api/admin/groups", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestAdmin_ListGroupsAndCreate(t *testing.T) {
	svc := &fakeAdmin{}
	s := newAdminServer(t, true, svc)

	resp := doReq(t, s, http.MethodGet, "/api/admin/groups", "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var groups []map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&groups))
	_ = resp.Body.Close()
	require.Len(t, groups, 1)
	require.EqualValues(t, 2, groups[0]["member_count"])

	resp = doReq(t, s, http.MethodPost, "/api/admin/groups", `{"name":"sre"}`)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	require.Equal(t, "sre", readJSON(t, resp)["name"])
	require.Equal(t, pgUUID(callerUUID), svc.actor, "actor is the verified caller")
}

func TestAdmin_InvalidUUIDIs400(t *testing.T) {
	s := newAdminServer(t, true, &fakeAdmin{})
	for _, p := range []string{"/api/admin/groups/nope", "/api/admin/groups/nope/members/" + uuid.NewString()} {
		resp := doReq(t, s, http.MethodDelete, p, "")
		require.Equal(t, http.StatusBadRequest, resp.StatusCode, p)
		_ = resp.Body.Close()
	}
}

func TestAdmin_ErrorMapping(t *testing.T) {
	gid := uuid.NewString()
	cases := []struct {
		err    error
		status int
		body   string
	}{
		{admin.ErrNotFound, http.StatusNotFound, "not found"},
		{admin.ErrGroupNameTaken, http.StatusConflict, admin.ErrGroupNameTaken.Error()},
		{&admin.ValidationError{Message: "group name is required"}, http.StatusUnprocessableEntity, "group name is required"},
	}
	for _, tc := range cases {
		s := newAdminServer(t, true, &fakeAdmin{err: tc.err})
		resp := doReq(t, s, http.MethodPatch, "/api/admin/groups/"+gid, `{"name":"x"}`)
		require.Equal(t, tc.status, resp.StatusCode)
		require.Equal(t, tc.body, readJSON(t, resp)["error"])
	}
}

func TestAdmin_GrantTagNeedsConfirmation409(t *testing.T) {
	svc := &fakeAdmin{err: &admin.NeedsConfirmationError{Tag: "secret"}}
	s := newAdminServer(t, true, svc)

	resp := doReq(t, s, http.MethodPost, "/api/admin/groups/"+uuid.NewString()+"/tags", `{"tag":"secret"}`)
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	body := readJSON(t, resp)
	require.Equal(t, "needs_confirmation", body["error"])
	require.Equal(t, "secret", body["tag"])
	require.False(t, svc.confirm)

	svc.err = nil
	resp = doReq(t, s, http.MethodPost, "/api/admin/groups/"+uuid.NewString()+"/tags", `{"tag":"secret","confirm_protect":true}`)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	_ = resp.Body.Close()
	require.True(t, svc.confirm)
}

func TestAdmin_RevokeTagConfirmQuery(t *testing.T) {
	svc := &fakeAdmin{}
	s := newAdminServer(t, true, svc)
	p := "/api/admin/groups/" + uuid.NewString() + "/tags/" + uuid.NewString()

	resp := doReq(t, s, http.MethodDelete, p, "")
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	_ = resp.Body.Close()
	require.False(t, svc.confirm)

	resp = doReq(t, s, http.MethodDelete, p+"?confirm_unprotect=true", "")
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	_ = resp.Body.Close()
	require.True(t, svc.confirm)
}

func TestAdmin_AddMemberNeverLeaksPasswordHash(t *testing.T) {
	s := newAdminServer(t, true, &fakeAdmin{})
	resp := doReq(t, s, http.MethodPost, "/api/admin/groups/"+uuid.NewString()+"/members", `{"email":"ada@example.com"}`)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	body := readJSON(t, resp)
	require.Equal(t, "ada@example.com", body["email"])
	require.NotContains(t, body, "password_hash")
}

func TestAdmin_SetSuperAdmin(t *testing.T) {
	svc := &fakeAdmin{}
	s := newAdminServer(t, true, svc)
	p := "/api/admin/users/" + uuid.NewString() + "/super-admin"

	resp := doReq(t, s, http.MethodPut, p, `{}`)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode, "missing value is not a silent revoke")
	_ = resp.Body.Close()

	resp = doReq(t, s, http.MethodPut, p, `{"value":true}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, true, readJSON(t, resp)["is_super_admin"])

	svc.err = admin.ErrLastSuperAdmin
	resp = doReq(t, s, http.MethodPut, p, `{"value":false}`)
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	require.Equal(t, admin.ErrLastSuperAdmin.Error(), readJSON(t, resp)["error"])
	require.False(t, svc.superValue)
}

func TestMe_IncludesSuperAdminFlag(t *testing.T) {
	for _, super := range []bool{true, false} {
		s := newAdminServer(t, super, nil)
		resp := doReq(t, s, http.MethodGet, "/api/v1/me", "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.Equal(t, super, readJSON(t, resp)["is_super_admin"])
	}
}
