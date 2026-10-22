//go:build integration

package db_test

import (
	"context"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/Learistocats/LookupKB/api/internal/admin"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
)

func auditCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, action string) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM audit_log WHERE action = $1", action).Scan(&n))
	return n
}

// TestAdminGroups exercises the group/member/tag-grant queries through the
// real admin service (real transactions, real constraints and cascades).
func TestAdminGroups(t *testing.T) {
	ctx := context.Background()
	pool, actor := newFixture(t, ctx)
	q := dbgen.New(pool)
	svc := admin.New(pool)

	member, err := q.CreateUser(ctx, dbgen.CreateUserParams{Email: "Ada@Example.com", PasswordHash: "x"})
	require.NoError(t, err)

	g, err := svc.CreateGroup(ctx, actor, " ops ")
	require.NoError(t, err)
	require.Equal(t, "ops", g.Name)
	_, err = svc.CreateGroup(ctx, actor, "ops")
	require.ErrorIs(t, err, admin.ErrGroupNameTaken, "UNIQUE(name) maps to a conflict")

	g, err = svc.RenameGroup(ctx, actor, g.ID, "sre")
	require.NoError(t, err)
	require.Equal(t, "sre", g.Name)

	// Members by (case-insensitive) email; idempotent re-add.
	_, err = svc.AddMember(ctx, actor, g.ID, "ada@example.com")
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, actor, g.ID, "ADA@example.com")
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, actor, g.ID, "nobody@example.com")
	require.ErrorIs(t, err, admin.ErrNotFound)
	require.Equal(t, 1, auditCount(t, ctx, pool, admin.ActionGroupMemberAdd))

	// Tag policy: first grant needs confirmation and leaves no tag row behind.
	_, err = svc.GrantTag(ctx, actor, g.ID, "Top Secret", false)
	var cerr *admin.NeedsConfirmationError
	require.ErrorAs(t, err, &cerr)
	require.Equal(t, "top-secret", cerr.Tag)
	var tagRows int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM tags WHERE slug = 'top-secret'").Scan(&tagRows))
	require.Zero(t, tagRows, "the unconfirmed upsert rolled back")

	tag, err := svc.GrantTag(ctx, actor, g.ID, "Top Secret", true)
	require.NoError(t, err)
	n, err := q.CountTagGrants(ctx, tag.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)

	rows, err := q.ListGroups(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.EqualValues(t, 1, rows[0].MemberCount)
	require.EqualValues(t, 1, rows[0].TagCount)

	d, err := svc.GetGroup(ctx, g.ID)
	require.NoError(t, err)
	require.Len(t, d.Members, 1)
	require.Equal(t, member.ID, d.Members[0].ID)
	require.Len(t, d.Tags, 1)
	require.Equal(t, "top-secret", d.Tags[0].Label)

	// Last grant revoke needs confirmation; the grant survives the refusal.
	err = svc.RevokeTag(ctx, actor, g.ID, tag.ID, false)
	require.ErrorAs(t, err, &cerr)
	n, err = q.CountTagGrants(ctx, tag.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	require.NoError(t, svc.RevokeTag(ctx, actor, g.ID, tag.ID, true))
	n, err = q.CountTagGrants(ctx, tag.ID)
	require.NoError(t, err)
	require.Zero(t, n)

	// Delete cascades to members and grants.
	_, err = svc.GrantTag(ctx, actor, g.ID, "top-secret", true)
	require.NoError(t, err)
	require.NoError(t, svc.RemoveMember(ctx, actor, g.ID, member.ID))
	_, err = svc.AddMember(ctx, actor, g.ID, "ada@example.com")
	require.NoError(t, err)
	require.NoError(t, svc.DeleteGroup(ctx, actor, g.ID))
	var leftovers int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT (SELECT count(*) FROM group_members) + (SELECT count(*) FROM group_tag_grants)").Scan(&leftovers))
	require.Zero(t, leftovers)
	_, err = q.GetGroup(ctx, g.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)

	for _, a := range []string{
		admin.ActionGroupCreate, admin.ActionGroupRename, admin.ActionGroupDelete,
		admin.ActionGroupMemberRemove, admin.ActionGroupTagGrant, admin.ActionGroupTagRevoke,
	} {
		require.Positive(t, auditCount(t, ctx, pool, a), a)
	}
	var actorMatches bool
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT bool_and(actor_id = $1) FROM audit_log WHERE action LIKE 'group.%'", actor).Scan(&actorMatches))
	require.True(t, actorMatches, "every group audit row carries the actor")
}

// TestSuperAdmin covers the role queries, the CLI path and the last-admin guard,
// including two concurrent demotions (LockSuperAdmins serializes them).
func TestSuperAdmin(t *testing.T) {
	ctx := context.Background()
	pool, a := newFixture(t, ctx)
	q := dbgen.New(pool)
	svc := admin.New(pool)

	is, err := q.GetUserIsSuperAdmin(ctx, a)
	require.NoError(t, err)
	require.False(t, is, "defaults to false")

	// CLI bootstrap: null actor.
	row, err := svc.SetSuperAdminByEmail(ctx, "AUTHOR@example.com", true)
	require.NoError(t, err)
	require.Equal(t, a, row.ID)
	var nullActor bool
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT actor_id IS NULL FROM audit_log WHERE action = 'user.super_admin_set'").Scan(&nullActor))
	require.True(t, nullActor)

	_, err = svc.SetSuperAdmin(ctx, a, a, false)
	require.ErrorIs(t, err, admin.ErrLastSuperAdmin)

	b, err := q.CreateUser(ctx, dbgen.CreateUserParams{Email: "b@example.com", PasswordHash: "x"})
	require.NoError(t, err)
	_, err = svc.SetSuperAdmin(ctx, a, b.ID, true)
	require.NoError(t, err)
	n, err := q.CountSuperAdmins(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 2, n)

	users, err := q.ListUsers(ctx)
	require.NoError(t, err)
	require.Len(t, users, 2)

	// Two admins demote each other at the same time: exactly one must win.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, pair := range [][2]pgtype.UUID{{a, b.ID}, {b.ID, a}} {
		wg.Add(1)
		go func(i int, actor, target pgtype.UUID) {
			defer wg.Done()
			_, errs[i] = svc.SetSuperAdmin(ctx, actor, target, false)
		}(i, pair[0], pair[1])
	}
	wg.Wait()
	n, err = q.CountSuperAdmins(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, n, "the guard holds under concurrency (errs: %v)", errs)

	_, err = svc.SetSuperAdminByEmail(ctx, "nobody@example.com", true)
	require.ErrorIs(t, err, admin.ErrNotFound)
}
