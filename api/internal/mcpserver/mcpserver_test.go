package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Learistocats/LookupKB/api/internal/access"
	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/Learistocats/LookupKB/api/internal/catalog"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/Learistocats/LookupKB/api/internal/search"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

var testUser = uuid.MustParse("6f1c2a52-6a0e-4b9c-9a39-1b7f6b0c2d11")

type fakeCatalog struct {
	docs      map[string]catalog.Document
	page      catalog.Page
	listErr   error
	gotCaller access.Caller
	gotLimit  int
	gotCursor string
}

func (f *fakeCatalog) Get(_ context.Context, c access.Caller, slug string) (catalog.Document, error) {
	f.gotCaller = c
	d, ok := f.docs[slug]
	if !ok {
		return catalog.Document{}, catalog.ErrNotFound
	}
	return d, nil
}

func (f *fakeCatalog) List(_ context.Context, c access.Caller, limit int, cursor string) (catalog.Page, error) {
	f.gotCaller, f.gotLimit, f.gotCursor = c, limit, cursor
	return f.page, f.listErr
}

type fakeSearch struct {
	res    search.Result
	err    error
	query  search.Query
	caller access.Caller
}

func (f *fakeSearch) Search(_ context.Context, q search.Query, c access.Caller) (search.Result, error) {
	f.query, f.caller = q, c
	return f.res, f.err
}

type fakeTags struct {
	rows     []dbgen.SearchTagsRow
	gotQuery string
	gotLimit int32
	caller   access.Caller
}

func (f *fakeTags) SearchTags(_ context.Context, q string, limit int32, c access.Caller) ([]dbgen.SearchTagsRow, error) {
	f.gotQuery, f.gotLimit, f.caller = q, limit, c
	return f.rows, nil
}

type harness struct {
	t      *testing.T
	app    *fiber.App
	token  string
	cat    *fakeCatalog
	search *fakeSearch
	tags   *fakeTags
}

func newHarness(t *testing.T, origins ...string) *harness {
	t.Helper()
	tokens, err := auth.NewTokens("0123456789abcdef0123456789abcdef", time.Hour)
	require.NoError(t, err)
	raw, _, err := tokens.Issue(testUser.String(), "agent@example.com")
	require.NoError(t, err)

	md := "# Kube\n\nRun **it**."
	h := &harness{
		t:     t,
		token: "Bearer " + raw,
		cat: &fakeCatalog{docs: map[string]catalog.Document{
			"kube-1": {
				Summary: catalog.Summary{Slug: "kube-1", Title: "Kube", Description: "How to", Tags: []string{"ops"},
					AuthorDisplayName: "ada", PublishedAt: "2026-10-01T00:00:00Z", UpdatedAt: "2026-10-02T00:00:00Z"},
				HTML: "<h1>Kube</h1><p>Run <strong>it</strong>.</p>", Markdown: md,
			},
		}},
		search: &fakeSearch{},
		tags:   &fakeTags{},
	}
	log := slog.New(slog.DiscardHandler)
	handler, err := New(h.cat, h.search, h.tags, Options{Version: "test", AllowedOrigins: origins, Logger: log})
	require.NoError(t, err)
	h.app = fiber.New(fiber.Config{DisableStartupMessage: true})
	h.app.All("/mcp", auth.NewMiddleware(tokens, nil, log).Handle, handler.Handle)
	return h
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (h *harness) post(body string, headers map[string]string) (int, http.Header, []byte) {
	h.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Authorization", h.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := h.app.Test(req, -1)
	require.NoError(h.t, err)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, b
}

func (h *harness) rpc(body string) rpcResponse {
	h.t.Helper()
	code, hdr, b := h.post(body, map[string]string{"MCP-Protocol-Version": "2025-06-18"})
	require.Equal(h.t, http.StatusOK, code, string(b))
	require.Contains(h.t, hdr.Get("Content-Type"), "application/json")
	var r rpcResponse
	require.NoError(h.t, json.Unmarshal(b, &r), string(b))
	require.Equal(h.t, "2.0", r.JSONRPC)
	return r
}

type toolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent"`
	IsError           bool            `json:"isError"`
}

func (h *harness) call(tool string, args string) toolResult {
	h.t.Helper()
	r := h.rpc(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"` + tool + `","arguments":` + args + `}}`)
	require.Nil(h.t, r.Error)
	var res toolResult
	require.NoError(h.t, json.Unmarshal(r.Result, &res))
	require.NotEmpty(h.t, res.Content)
	require.Equal(h.t, "text", res.Content[0].Type)
	return res
}

func TestInitializeNegotiatesAndAdvertisesTools(t *testing.T) {
	h := newHarness(t)
	code, _, b := h.post(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`, nil)
	require.Equal(t, http.StatusOK, code, string(b))
	var r rpcResponse
	require.NoError(t, json.Unmarshal(b, &r))
	var init struct {
		ProtocolVersion string `json:"protocolVersion"`
		Capabilities    struct {
			Tools json.RawMessage `json:"tools"`
		} `json:"capabilities"`
		ServerInfo struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
		Instructions string `json:"instructions"`
	}
	require.NoError(t, json.Unmarshal(r.Result, &init))
	require.Equal(t, "2025-06-18", init.ProtocolVersion)
	require.NotNil(t, init.Capabilities.Tools)
	require.Equal(t, "lookupkb", init.ServerInfo.Name)
	require.Equal(t, "test", init.ServerInfo.Version)
	require.NotEmpty(t, init.Instructions)

	// An unknown client version is answered with one the server supports.
	r = h.rpc(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"1999-01-01","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`)
	require.NoError(t, json.Unmarshal(r.Result, &init))
	require.NotEqual(t, "1999-01-01", init.ProtocolVersion)

	// notifications/initialized: 202, no body.
	code, _, b = h.post(`{"jsonrpc":"2.0","method":"notifications/initialized"}`, map[string]string{"MCP-Protocol-Version": "2025-06-18"})
	require.Equal(t, http.StatusAccepted, code)
	require.Empty(t, strings.TrimSpace(string(b)))

	r = h.rpc(`{"jsonrpc":"2.0","id":"p","method":"ping"}`)
	require.Nil(t, r.Error)
	require.JSONEq(t, `"p"`, string(r.ID))
}

func TestToolsListSchemas(t *testing.T) {
	h := newHarness(t)
	r := h.rpc(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	var list struct {
		Tools []struct {
			Name        string `json:"name"`
			InputSchema struct {
				Type       string                     `json:"type"`
				Required   []string                   `json:"required"`
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"inputSchema"`
			Annotations struct {
				ReadOnlyHint bool `json:"readOnlyHint"`
			} `json:"annotations"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(r.Result, &list))
	byName := map[string]int{}
	for i, tl := range list.Tools {
		byName[tl.Name] = i
		require.Equal(t, "object", tl.InputSchema.Type)
		require.True(t, tl.Annotations.ReadOnlyHint, tl.Name)
	}
	require.Len(t, list.Tools, 4)
	s := list.Tools[byName[ToolSearch]].InputSchema
	require.Equal(t, []string{"query"}, s.Required)
	require.Contains(t, string(s.Properties["limit"]), `"maximum":50`)
	g := list.Tools[byName[ToolGetDocument]].InputSchema
	require.Equal(t, []string{"slug"}, g.Required)
	require.Contains(t, string(g.Properties["format"]), `"enum":["markdown","text","html"]`)
	require.Empty(t, list.Tools[byName[ToolListDocuments]].InputSchema.Required)
	require.Empty(t, list.Tools[byName[ToolListTags]].InputSchema.Required)
}

func TestJSONRPCErrors(t *testing.T) {
	h := newHarness(t)
	// An unknown method fails the HTTP request (400) on the 2025 transports —
	// the SDK pre-validates every message so nothing is silently dropped.
	code, _, b := h.post(`{"jsonrpc":"2.0","id":3,"method":"no/such"}`, map[string]string{"MCP-Protocol-Version": "2025-06-18"})
	require.Equal(t, http.StatusBadRequest, code)
	require.Equal(t, codeInvalidRequest, rpcErrorCode(t, b))
	require.Contains(t, string(b), `no/such`)

	r := h.rpc(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"no_such_tool","arguments":{}}}`)
	require.NotNil(t, r.Error)
	require.Equal(t, -32602, r.Error.Code)

	code, _, b = h.post(`{not json`, map[string]string{"MCP-Protocol-Version": "2025-06-18"})
	require.Equal(t, http.StatusBadRequest, code)
	require.Equal(t, codeInvalidRequest, rpcErrorCode(t, b))

	// Not a JSON-RPC 2.0 envelope.
	code, _, b = h.post(`{"jsonrpc":"1.0","id":1,"method":"ping"}`, map[string]string{"MCP-Protocol-Version": "2025-06-18"})
	require.Equal(t, http.StatusBadRequest, code)
	require.Equal(t, codeInvalidRequest, rpcErrorCode(t, b))

	// Bad arguments are a tool error the agent can read and correct.
	res := h.call(ToolSearch, `{"query":"x","limit":500}`)
	require.True(t, res.IsError)
	res = h.call(ToolGetDocument, `{}`)
	require.True(t, res.IsError)
}

func TestSearchTool(t *testing.T) {
	h := newHarness(t)
	h.search.res = search.Result{Hits: []search.Hit{{
		Slug: "kube-1", Title: "Kube", Snippet: "run <mark>kube</mark> &amp; co", Tags: []string{"ops"}, PublishedAt: "2026-10-01T00:00:00Z",
	}}}
	res := h.call(ToolSearch, `{"query":"kube","tags":["ops"],"limit":5}`)
	require.False(t, res.IsError)
	require.Equal(t, "kube", h.search.query.Q)
	require.Equal(t, 5, h.search.query.Limit)
	require.Equal(t, []string{"ops"}, h.search.query.Filters.Tags)
	require.Equal(t, testUser, uuidOf(h.search.caller), "runs as the verified caller")
	require.Contains(t, res.Content[0].Text, "Kube (slug: kube-1)")
	require.Contains(t, res.Content[0].Text, "run kube & co")
	require.JSONEq(t, `{"hits":[{"slug":"kube-1","title":"Kube","snippet":"run kube & co","tags":["ops"],"published_at":"2026-10-01T00:00:00Z"}]}`,
		string(res.StructuredContent))

	h.call(ToolSearch, `{"query":"x"}`)
	require.Equal(t, searchDefaultLimit, h.search.query.Limit)

	h.search.err = search.ErrInvalidFilter
	res = h.call(ToolSearch, `{"query":"x"}`)
	require.True(t, res.IsError)
	require.Equal(t, "invalid search filter", res.Content[0].Text)

	h.search.err = errors.New("meili: dial tcp 10.0.0.3:7700: refused")
	res = h.call(ToolSearch, `{"query":"x"}`)
	require.True(t, res.IsError)
	require.Equal(t, "internal error", res.Content[0].Text, "no internal detail leaks")
}

func TestGetDocumentTool(t *testing.T) {
	h := newHarness(t)
	res := h.call(ToolGetDocument, `{"slug":"kube-1"}`)
	require.False(t, res.IsError)
	text := res.Content[0].Text
	require.True(t, strings.HasPrefix(text, "---\ntitle: \"Kube\"\nslug: \"kube-1\"\n"), text)
	require.Contains(t, text, "tags: [\"ops\"]\n")
	require.Contains(t, text, "format: markdown\n---\n\n# Kube\n\nRun **it**.")
	var meta documentMeta
	require.NoError(t, json.Unmarshal(res.StructuredContent, &meta))
	require.Equal(t, "kube-1", meta.Slug)
	require.Equal(t, "markdown", meta.Format)
	require.NotContains(t, string(res.StructuredContent), "Run **it**", "the body is not duplicated in structuredContent")

	res = h.call(ToolGetDocument, `{"slug":"kube-1","format":"text"}`)
	require.True(t, strings.HasSuffix(res.Content[0].Text, "---\n\nKube\n\nRun it."), res.Content[0].Text)
	res = h.call(ToolGetDocument, `{"slug":"kube-1","format":"html"}`)
	require.True(t, strings.HasSuffix(res.Content[0].Text, "<h1>Kube</h1><p>Run <strong>it</strong>.</p>"))
	res = h.call(ToolGetDocument, `{"slug":"kube-1","format":"pdf"}`)
	require.True(t, res.IsError, "format is an enum")

	// Missing and inaccessible are the same "not found" (the catalog does not
	// distinguish them; neither does the tool).
	res = h.call(ToolGetDocument, `{"slug":"hidden-or-missing"}`)
	require.True(t, res.IsError)
	require.Equal(t, "document not found", res.Content[0].Text)
	require.Equal(t, testUser, uuidOf(h.cat.gotCaller))
}

func TestListDocumentsTool(t *testing.T) {
	h := newHarness(t)
	h.cat.page = catalog.Page{
		Documents:  []catalog.Summary{{Slug: "a", Title: "A", Tags: []string{"x"}, PublishedAt: "2026-10-01T00:00:00Z"}},
		NextCursor: "CUR",
	}
	res := h.call(ToolListDocuments, `{"limit":1,"cursor":"PREV"}`)
	require.False(t, res.IsError)
	require.Equal(t, 1, h.cat.gotLimit)
	require.Equal(t, "PREV", h.cat.gotCursor)
	require.Contains(t, res.Content[0].Text, "- A (slug: a)")
	require.Contains(t, res.Content[0].Text, "next_cursor: CUR")
	require.Contains(t, string(res.StructuredContent), `"next_cursor":"CUR"`)

	h.cat.listErr = catalog.ErrInvalidCursor
	res = h.call(ToolListDocuments, `{"cursor":"bad"}`)
	require.True(t, res.IsError)
	require.Equal(t, "invalid cursor", res.Content[0].Text)

	res = h.call(ToolListDocuments, `{"limit":101}`)
	require.True(t, res.IsError, "limit is bounded by the schema")
}

func TestListTagsTool(t *testing.T) {
	h := newHarness(t)
	h.tags.rows = []dbgen.SearchTagsRow{{Slug: "ops", Label: "ops", UsageCount: 3, IsProtected: true}}
	res := h.call(ToolListTags, `{"prefix":"op"}`)
	require.False(t, res.IsError)
	require.Equal(t, "op", h.tags.gotQuery)
	require.EqualValues(t, tagsLimit, h.tags.gotLimit)
	require.Equal(t, testUser, uuidOf(h.tags.caller))
	require.JSONEq(t, `{"tags":[{"slug":"ops","label":"ops","usage_count":3,"protected":true}]}`, string(res.StructuredContent))
}

func TestTransportGuards(t *testing.T) {
	h := newHarness(t, "http://localhost:6274")

	// No credential → 401 from the auth middleware, before any JSON-RPC.
	req, _ := http.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	resp, err := h.app.Test(req, -1)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// A foreign browser Origin is refused; an allowlisted one passes.
	code, _, _ := h.post(`{"jsonrpc":"2.0","id":1,"method":"ping"}`, map[string]string{"Origin": "https://evil.example"})
	require.Equal(t, http.StatusForbidden, code)
	code, _, _ = h.post(`{"jsonrpc":"2.0","id":1,"method":"ping"}`, map[string]string{"Origin": "http://localhost:6274", "MCP-Protocol-Version": "2025-06-18"})
	require.Equal(t, http.StatusOK, code)

	// Oversized body → 413.
	big := `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"pad":"` + strings.Repeat("x", MaxBodyBytes) + `"}}`
	code, _, _ = h.post(big, nil)
	require.Equal(t, http.StatusRequestEntityTooLarge, code)

	// Unsupported protocol version header → 400.
	code, _, _ = h.post(`{"jsonrpc":"2.0","id":1,"method":"ping"}`, map[string]string{"MCP-Protocol-Version": "2000-01-01"})
	require.Equal(t, http.StatusBadRequest, code)

	// No server-initiated stream in stateless mode: GET → 405.
	req, _ = http.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Authorization", h.token)
	req.Header.Set("Accept", "text/event-stream")
	resp, err = h.app.Test(req, -1)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func uuidOf(c access.Caller) uuid.UUID { return uuid.UUID(c.UserID.Bytes) }

func rpcErrorCode(t *testing.T, body []byte) int {
	t.Helper()
	var r rpcResponse
	require.NoError(t, json.Unmarshal(body, &r), string(body))
	require.NotNil(t, r.Error, string(body))
	require.Equal(t, "null", string(r.ID))
	return r.Error.Code
}
