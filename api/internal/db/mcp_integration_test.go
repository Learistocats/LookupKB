//go:build integration

package db_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// mcpCall posts one JSON-RPC message to /mcp as a Streamable HTTP client would.
func mcpCall(t *testing.T, h *apiHarness, authz, body string) (int, []byte) {
	t.Helper()
	return h.call(authz, http.MethodPost, "/mcp", body, map[string]string{
		"Accept":               "application/json, text/event-stream",
		"MCP-Protocol-Version": "2025-06-18",
	})
}

type mcpToolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent"`
	IsError           bool            `json:"isError"`
}

func mcpTool(t *testing.T, h *apiHarness, authz, tool, args string) mcpToolResult {
	t.Helper()
	code, b := mcpCall(t, h, authz, `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"`+tool+`","arguments":`+args+`}}`)
	require.Equal(t, http.StatusOK, code, string(b))
	var resp struct {
		Result mcpToolResult `json:"result"`
		Error  any           `json:"error"`
	}
	require.NoError(t, json.Unmarshal(b, &resp), string(b))
	require.Nil(t, resp.Error, string(b))
	require.NotEmpty(t, resp.Result.Content)
	return resp.Result
}

// TestMCPEndToEnd drives the MCP endpoint over HTTP with real PATs, Postgres,
// Meilisearch and the outbox worker: initialize → tools/list → tools/call, the
// protected-tag denial (the same rule as the public API), and 401 for a missing
// or revoked PAT.
func TestMCPEndToEnd(t *testing.T) {
	h := newAPIHarness(t)
	alice := h.user("alice@example.com") // in the protected group
	bob := h.user("bob@example.com")     // not

	_, secretSlug := h.publish(alice, "Basalt secret plan", "# Basalt\n\nbasalt secret body", "basalt-team")
	_, openSlug := h.publish(alice, "Basalt public notes", "# Notes\n\nbasalt public body")
	h.protect("basalt-team", "alice@example.com")
	alicePAT, _ := h.pat(alice)
	bobPAT, bobPATID := h.pat(bob)

	// initialize → notifications/initialized → tools/list.
	code, b := mcpCall(t, h, bobPAT, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"it","version":"1"}}}`)
	require.Equal(t, http.StatusOK, code, string(b))
	require.Contains(t, string(b), `"protocolVersion":"2025-06-18"`)
	require.Contains(t, string(b), `"name":"lookupkb"`)
	code, _ = mcpCall(t, h, bobPAT, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	require.Equal(t, http.StatusAccepted, code)
	code, b = mcpCall(t, h, bobPAT, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	require.Equal(t, http.StatusOK, code)
	for _, name := range []string{"search", "get_document", "list_documents", "list_tags"} {
		require.Contains(t, string(b), `"name":"`+name+`"`)
	}

	// search: once both entries are indexed, alice finds both, bob only the open one.
	require.Eventually(t, func() bool {
		res := mcpTool(t, h, alicePAT, "search", `{"query":"basalt"}`)
		return strings.Contains(res.Content[0].Text, secretSlug) && strings.Contains(res.Content[0].Text, openSlug)
	}, 20*time.Second, 200*time.Millisecond)
	res := mcpTool(t, h, bobPAT, "search", `{"query":"basalt"}`)
	require.False(t, res.IsError)
	require.Contains(t, res.Content[0].Text, openSlug)
	require.NotContains(t, res.Content[0].Text, secretSlug)
	require.NotContains(t, string(res.StructuredContent), secretSlug)

	// get_document: Markdown by default for a readable entry …
	res = mcpTool(t, h, bobPAT, "get_document", `{"slug":"`+openSlug+`"}`)
	require.False(t, res.IsError)
	require.True(t, strings.HasSuffix(res.Content[0].Text, "# Notes\n\nbasalt public body"), res.Content[0].Text)
	// … and the protected one is exactly "not found" for bob, like a missing slug.
	denied := mcpTool(t, h, bobPAT, "get_document", `{"slug":"`+secretSlug+`"}`)
	missing := mcpTool(t, h, bobPAT, "get_document", `{"slug":"no-such-slug"}`)
	require.True(t, denied.IsError)
	require.Equal(t, missing, denied)
	require.Equal(t, "document not found", denied.Content[0].Text)
	res = mcpTool(t, h, alicePAT, "get_document", `{"slug":"`+secretSlug+`","format":"text"}`)
	require.False(t, res.IsError)
	require.True(t, strings.HasSuffix(res.Content[0].Text, "Basalt\n\nbasalt secret body"), res.Content[0].Text)

	// list_documents / list_tags apply the same rule.
	res = mcpTool(t, h, bobPAT, "list_documents", `{}`)
	require.Contains(t, res.Content[0].Text, openSlug)
	require.NotContains(t, res.Content[0].Text, secretSlug)
	res = mcpTool(t, h, bobPAT, "list_tags", `{"prefix":"basalt"}`)
	require.NotContains(t, string(res.StructuredContent), "basalt-team")
	res = mcpTool(t, h, alicePAT, "list_tags", `{"prefix":"basalt"}`)
	require.Contains(t, string(res.StructuredContent), `"slug":"basalt-team"`)

	// 401 without a PAT, and once the PAT is revoked.
	code, _ = mcpCall(t, h, "", `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	require.Equal(t, http.StatusUnauthorized, code)
	code, _ = h.call(h.session(bob), http.MethodDelete, "/api/tokens/"+bobPATID, "", nil)
	require.Equal(t, http.StatusNoContent, code)
	code, _ = mcpCall(t, h, bobPAT, `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	require.Equal(t, http.StatusUnauthorized, code)
}
