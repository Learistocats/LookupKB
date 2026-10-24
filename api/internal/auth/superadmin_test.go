package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func identityFor(t *testing.T, store Store) (int, Identity) {
	t.Helper()
	tk := newTestTokens(t)
	app := newTestApp(NewMiddleware(tk, store, discardLogger()))
	raw, _, err := tk.Issue(uuid.NewString(), "user@example.com")
	require.NoError(t, err)
	resp := doGet(t, app, "Bearer "+raw)
	defer resp.Body.Close()
	var id Identity
	if resp.StatusCode == http.StatusOK {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&id))
	}
	return resp.StatusCode, id
}

func TestMiddleware_LoadsSuperAdminPerRequest(t *testing.T) {
	d := &fakeStore{super: true}
	status, id := identityFor(t, d)
	require.Equal(t, http.StatusOK, status)
	require.True(t, id.IsSuperAdmin)
	require.Equal(t, 1, d.superHits, "flag is read from the store on every request")

	d.super = false // revoked: honored on the very next request
	_, id = identityFor(t, d)
	require.False(t, id.IsSuperAdmin)
}

func TestMiddleware_SuperAdminDefaultsFalse(t *testing.T) {
	// No store: never an admin.
	_, id := identityFor(t, nil)
	require.False(t, id.IsSuperAdmin)
	// Subject no longer exists.
	_, id = identityFor(t, &fakeStore{superErr: pgx.ErrNoRows})
	require.False(t, id.IsSuperAdmin)
}

func TestMiddleware_SuperAdminLoadErrorIs500(t *testing.T) {
	status, _ := identityFor(t, &fakeStore{superErr: errors.New("db down")})
	require.Equal(t, http.StatusInternalServerError, status)
}

func TestRequireSuperAdmin(t *testing.T) {
	run := func(id *Identity) int {
		app := fiber.New()
		app.Get("/x", func(c *fiber.Ctx) error {
			if id != nil {
				c.Locals(identityLocalsKey, *id)
			}
			return c.Next()
		}, RequireSuperAdmin, func(c *fiber.Ctx) error { return c.SendStatus(http.StatusNoContent) })
		req, _ := http.NewRequest(http.MethodGet, "/x", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	require.Equal(t, http.StatusForbidden, run(nil))
	require.Equal(t, http.StatusForbidden, run(&Identity{}))
	require.Equal(t, http.StatusNoContent, run(&Identity{IsSuperAdmin: true}))
}
