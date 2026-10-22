package server

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/Learistocats/LookupKB/api/internal/config"
	"github.com/stretchr/testify/require"
)

const mcpInitialize = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`

func postMCP(t *testing.T, s *Server, method, authz, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, "/mcp", strings.NewReader(body))
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := s.App().Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// /mcp exists only when auth + documents + search + tags are all configured, and
// a PAT may use it (POST; DELETE is off the PAT surface).
func TestMCPRoute_GatedAndPATReachable(t *testing.T) {
	cfg, err := config.Load()
	require.NoError(t, err)

	// Documents only: no MCP endpoint.
	code, _ := postMCP(t, newDraftServer(t, &fakeDocStore{}), http.MethodPost, bearer(t), mcpInitialize)
	require.Equal(t, http.StatusNotFound, code)

	tokens := newFakeTokenStore()
	mw := auth.NewMiddleware(testTokens, tokens, slogDiscard())
	s := New(cfg, slogDiscard(), "test", WithAuth(mw), WithTokens(tokens), WithDocuments(&fakeDocStore{}),
		WithSearch(&fakeSearch{}), WithTags(&fakeTagStore{}))
	pat := "Bearer " + createToken(t, s, `{"name":"agent"}`).Token

	code, body := postMCP(t, s, http.MethodPost, pat, mcpInitialize)
	require.Equal(t, http.StatusOK, code, body)
	require.Contains(t, body, `"serverInfo"`)

	code, _ = postMCP(t, s, http.MethodPost, bearer(t), mcpInitialize)
	require.Equal(t, http.StatusOK, code, "a session may use it too")

	code, _ = postMCP(t, s, http.MethodPost, "", mcpInitialize)
	require.Equal(t, http.StatusUnauthorized, code)

	code, _ = postMCP(t, s, http.MethodDelete, pat, "")
	require.Equal(t, http.StatusForbidden, code, "only GET/POST are on the PAT surface")
}
