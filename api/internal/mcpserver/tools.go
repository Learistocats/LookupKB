// Package mcpserver is LookupKB's MCP server for agents: a read-only Model
// Context Protocol endpoint over the Streamable HTTP transport, stateless, JSON
// responses only (no SSE stream).
//
// It is built on the official Go SDK (github.com/modelcontextprotocol/go-sdk),
// which owns the protocol: JSON-RPC 2.0 framing and error codes, initialize and
// protocol-version negotiation, the MCP-Protocol-Version header check, tools/list
// with JSON Schemas inferred from the input/output types below, argument
// validation, and isError tool results. This package only defines the four read
// tools and the Fiber bridge (http.go).
//
// Every tool runs as the verified caller — the PAT's (or session's) user, put on
// the request context by the bridge — and goes through the same services as the
// public read API (internal/catalog, search.Service, the caller-aware tag store),
// so the protected-tag rule applies identically: a hidden entry is "not found",
// never a distinct error. There are no write tools.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"strings"

	"github.com/Learistocats/LookupKB/api/internal/access"
	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/Learistocats/LookupKB/api/internal/catalog"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/Learistocats/LookupKB/api/internal/search"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Catalog is the published-read service (internal/catalog.Service).
type Catalog interface {
	Get(ctx context.Context, caller access.Caller, slug string) (catalog.Document, error)
	List(ctx context.Context, caller access.Caller, limit int, cursor string) (catalog.Page, error)
}

// Searcher is the access-filtered search service (search.Service).
type Searcher interface {
	Search(ctx context.Context, q search.Query, caller access.Caller) (search.Result, error)
}

// Tags is the caller-aware tag lookup (tags.Store).
type Tags interface {
	SearchTags(ctx context.Context, query string, limit int32, caller access.Caller) ([]dbgen.SearchTagsRow, error)
}

// Tool bounds.
const (
	searchDefaultLimit = 10
	searchMaxLimit     = search.MaxLimit // 50
	listMaxLimit       = catalog.MaxLimit
	tagsLimit          = 20 // the tag store's own cap
)

const instructions = `LookupKB is a knowledge base of published Markdown entries, organised by tags (no folders).
Use "search" to find entries by text (optionally narrowed by tags), "get_document" to read one by slug
(Markdown by default), "list_documents" to page through everything newest first, and "list_tags" to
discover the tag vocabulary. Everything is read-only and limited to what the token's user may read.`

// Tool names.
const (
	ToolSearch        = "search"
	ToolGetDocument   = "get_document"
	ToolListDocuments = "list_documents"
	ToolListTags      = "list_tags"
)

type tools struct {
	catalog Catalog
	search  Searcher
	tags    Tags
	log     *slog.Logger
}

// newMCPServer builds the SDK server with the four read tools registered.
func newMCPServer(t *tools, version string) (*mcp.Server, error) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "lookupkb", Title: "LookupKB", Version: version}, &mcp.ServerOptions{
		Instructions: instructions,
		Logger:       t.log,
	})
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: ptr(false)}

	searchSchema, err := inputSchema[searchInput](func(s *jsonschema.Schema) {
		bound(s.Properties["limit"], 1, searchMaxLimit)
	})
	if err != nil {
		return nil, err
	}
	getSchema, err := inputSchema[getDocumentInput](func(s *jsonschema.Schema) {
		f := s.Properties["format"]
		f.Enum = []any{"markdown", "text", "html"}
		f.Default = json.RawMessage(`"markdown"`)
	})
	if err != nil {
		return nil, err
	}
	listSchema, err := inputSchema[listDocumentsInput](func(s *jsonschema.Schema) {
		bound(s.Properties["limit"], 1, listMaxLimit)
	})
	if err != nil {
		return nil, err
	}
	tagsSchema, err := inputSchema[listTagsInput](nil)
	if err != nil {
		return nil, err
	}

	mcp.AddTool(srv, &mcp.Tool{
		Name:        ToolSearch,
		Title:       "Search the knowledge base",
		Description: "Full-text, typo-tolerant search over the published entries you can read. Returns the best hits with slug, title, a plain-text snippet, tags and publish date. Read a hit with get_document.",
		InputSchema: searchSchema,
		Annotations: readOnly,
	}, t.searchTool)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        ToolGetDocument,
		Title:       "Read a document",
		Description: "Fetch one published entry by slug: its metadata and its body as Markdown (default), plain text or sanitized HTML. Unknown and inaccessible slugs both answer \"document not found\".",
		InputSchema: getSchema,
		Annotations: readOnly,
	}, t.getDocumentTool)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        ToolListDocuments,
		Title:       "List documents",
		Description: "Page through the published entries you can read, newest first (metadata only). Pass next_cursor back as cursor for the next page.",
		InputSchema: listSchema,
		Annotations: readOnly,
	}, t.listDocumentsTool)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        ToolListTags,
		Title:       "List tags",
		Description: "The tags you can see, most used first, optionally filtered by a slug fragment. Use them to narrow search.",
		InputSchema: tagsSchema,
		Annotations: readOnly,
	}, t.listTagsTool)
	return srv, nil
}

// ---- search -------------------------------------------------------------

type searchInput struct {
	Query string   `json:"query" jsonschema:"Free-text query. Typo-tolerant; an empty string lists the newest entries."`
	Tags  []string `json:"tags,omitempty" jsonschema:"Only return entries carrying every one of these tags (slugs)."`
	Limit int      `json:"limit,omitempty" jsonschema:"Maximum number of hits (1-50, default 10)."`
}

type searchHit struct {
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Snippet     string   `json:"snippet"`
	Tags        []string `json:"tags"`
	PublishedAt string   `json:"published_at"`
}

type searchOutput struct {
	Hits []searchHit `json:"hits"`
}

func (t *tools) searchTool(ctx context.Context, _ *mcp.CallToolRequest, in searchInput) (*mcp.CallToolResult, searchOutput, error) {
	caller, err := callerFrom(ctx)
	if err != nil {
		return nil, searchOutput{}, err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = searchDefaultLimit
	}
	res, err := t.search.Search(ctx, search.Query{Q: in.Query, Limit: limit, Filters: search.Filters{Tags: in.Tags}}, caller)
	if err != nil {
		return nil, searchOutput{}, t.toolError(ctx, ToolSearch, err)
	}
	out := searchOutput{Hits: make([]searchHit, 0, len(res.Hits))}
	var b strings.Builder
	for i, h := range res.Hits {
		hit := searchHit{Slug: h.Slug, Title: h.Title, Snippet: plainSnippet(h.Snippet), Tags: h.Tags, PublishedAt: h.PublishedAt}
		out.Hits = append(out.Hits, hit)
		fmt.Fprintf(&b, "%d. %s (slug: %s)\n", i+1, hit.Title, hit.Slug)
		if len(hit.Tags) > 0 {
			fmt.Fprintf(&b, "   tags: %s\n", strings.Join(hit.Tags, ", "))
		}
		fmt.Fprintf(&b, "   published: %s\n", hit.PublishedAt)
		if hit.Snippet != "" {
			fmt.Fprintf(&b, "   %s\n", hit.Snippet)
		}
	}
	if len(out.Hits) == 0 {
		b.WriteString("No matching entries.")
	}
	return textResult(b.String()), out, nil
}

// plainSnippet turns the search snippet (escaped text + <mark>) into plain text.
func plainSnippet(s string) string {
	s = strings.NewReplacer("<mark>", "", "</mark>", "").Replace(s)
	return html.UnescapeString(s)
}

// ---- get_document --------------------------------------------------------

type getDocumentInput struct {
	Slug   string `json:"slug" jsonschema:"The entry's slug, as returned by search or list_documents."`
	Format string `json:"format,omitempty" jsonschema:"Body representation: markdown (the published source), text (plain text) or html (sanitized HTML)."`
}

// documentMeta is get_document's structured result: the metadata. The body is in
// the text content only, so a large entry is not sent twice.
type documentMeta struct {
	catalog.Summary
	Format string `json:"format"`
}

func (t *tools) getDocumentTool(ctx context.Context, _ *mcp.CallToolRequest, in getDocumentInput) (*mcp.CallToolResult, documentMeta, error) {
	caller, err := callerFrom(ctx)
	if err != nil {
		return nil, documentMeta{}, err
	}
	format := catalog.FormatMarkdown
	if in.Format != "" {
		f, ok := catalog.ParseFormat(in.Format)
		if !ok || f == catalog.FormatJSON {
			return nil, documentMeta{}, errors.New(`invalid format: use "markdown", "text" or "html"`)
		}
		format = f
	}
	doc, err := t.catalog.Get(ctx, caller, strings.TrimSpace(in.Slug))
	if err != nil {
		return nil, documentMeta{}, t.toolError(ctx, ToolGetDocument, err)
	}
	meta := documentMeta{Summary: doc.Summary, Format: string(format)}
	return textResult(frontMatter(meta) + doc.Body(format)), meta, nil
}

// frontMatter renders the metadata as a YAML front-matter block (values are JSON
// strings/arrays, which are valid YAML) ahead of the body.
func frontMatter(m documentMeta) string {
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	tags, _ := json.Marshal(m.Tags)
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "title: %s\n", q(m.Title))
	fmt.Fprintf(&b, "slug: %s\n", q(m.Slug))
	if m.Description != "" {
		fmt.Fprintf(&b, "description: %s\n", q(m.Description))
	}
	fmt.Fprintf(&b, "tags: %s\n", tags)
	fmt.Fprintf(&b, "author: %s\n", q(m.AuthorDisplayName))
	fmt.Fprintf(&b, "published_at: %s\n", q(m.PublishedAt))
	fmt.Fprintf(&b, "updated_at: %s\n", q(m.UpdatedAt))
	fmt.Fprintf(&b, "format: %s\n", m.Format)
	b.WriteString("---\n\n")
	return b.String()
}

// ---- list_documents -------------------------------------------------------

type listDocumentsInput struct {
	Limit  int    `json:"limit,omitempty" jsonschema:"Page size (1-100, default 20)."`
	Cursor string `json:"cursor,omitempty" jsonschema:"The next_cursor of the previous page; omit for the first page."`
}

func (t *tools) listDocumentsTool(ctx context.Context, _ *mcp.CallToolRequest, in listDocumentsInput) (*mcp.CallToolResult, catalog.Page, error) {
	caller, err := callerFrom(ctx)
	if err != nil {
		return nil, catalog.Page{}, err
	}
	page, err := t.catalog.List(ctx, caller, in.Limit, strings.TrimSpace(in.Cursor))
	if err != nil {
		return nil, catalog.Page{}, t.toolError(ctx, ToolListDocuments, err)
	}
	var b strings.Builder
	for _, d := range page.Documents {
		fmt.Fprintf(&b, "- %s (slug: %s) published %s", d.Title, d.Slug, d.PublishedAt)
		if len(d.Tags) > 0 {
			fmt.Fprintf(&b, " tags: %s", strings.Join(d.Tags, ", "))
		}
		b.WriteByte('\n')
	}
	if len(page.Documents) == 0 {
		b.WriteString("No entries.\n")
	}
	if page.NextCursor != "" {
		fmt.Fprintf(&b, "next_cursor: %s\n", page.NextCursor)
	}
	return textResult(b.String()), page, nil
}

// ---- list_tags -------------------------------------------------------------

type listTagsInput struct {
	Prefix string `json:"prefix,omitempty" jsonschema:"Only tags whose slug contains this fragment."`
}

type tagItem struct {
	Slug       string `json:"slug"`
	Label      string `json:"label"`
	UsageCount int32  `json:"usage_count"`
	Protected  bool   `json:"protected"`
}

type listTagsOutput struct {
	Tags []tagItem `json:"tags"`
}

func (t *tools) listTagsTool(ctx context.Context, _ *mcp.CallToolRequest, in listTagsInput) (*mcp.CallToolResult, listTagsOutput, error) {
	caller, err := callerFrom(ctx)
	if err != nil {
		return nil, listTagsOutput{}, err
	}
	rows, err := t.tags.SearchTags(ctx, in.Prefix, tagsLimit, caller)
	if err != nil {
		return nil, listTagsOutput{}, t.toolError(ctx, ToolListTags, err)
	}
	out := listTagsOutput{Tags: make([]tagItem, 0, len(rows))}
	var b strings.Builder
	for _, r := range rows {
		out.Tags = append(out.Tags, tagItem{Slug: r.Slug, Label: r.Label, UsageCount: r.UsageCount, Protected: r.IsProtected})
		fmt.Fprintf(&b, "- %s (%d entries)\n", r.Slug, r.UsageCount)
	}
	if len(rows) == 0 {
		b.WriteString("No tags.")
	}
	return textResult(b.String()), out, nil
}

// ---- helpers ---------------------------------------------------------------

// errUnauthenticated means the bridge did not put an identity on the context —
// an invariant break; the tool refuses rather than run as anyone.
var errUnauthenticated = errors.New("unauthenticated")

// callerFrom resolves the verified caller set by the HTTP bridge.
func callerFrom(ctx context.Context) (access.Caller, error) {
	id, ok := auth.IdentityFromContext(ctx)
	if !ok {
		return access.Caller{}, errUnauthenticated
	}
	uid, err := uuid.Parse(id.UserID)
	if err != nil {
		return access.Caller{}, errUnauthenticated
	}
	return access.Caller{UserID: pgtype.UUID{Bytes: uid, Valid: true}, IsSuperAdmin: id.IsSuperAdmin}, nil
}

// toolError maps a service error to the message the agent sees (an isError tool
// result). Client mistakes get a precise message; "not found" is the one answer
// for missing AND inaccessible entries; anything else is logged and reported
// generically, never with internal detail.
func (t *tools) toolError(ctx context.Context, tool string, err error) error {
	switch {
	case errors.Is(err, catalog.ErrNotFound):
		return errors.New("document not found")
	case errors.Is(err, catalog.ErrInvalidCursor):
		return errors.New("invalid cursor")
	case errors.Is(err, search.ErrInvalidFilter):
		return errors.New("invalid search filter")
	}
	t.log.ErrorContext(ctx, "mcp tool failed", "tool", tool, "error", err.Error())
	return errors.New("internal error")
}

func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

// inputSchema infers T's JSON Schema and lets the caller refine it (bounds, enum).
func inputSchema[T any](refine func(*jsonschema.Schema)) (*jsonschema.Schema, error) {
	s, err := jsonschema.For[T](nil)
	if err != nil {
		return nil, fmt.Errorf("mcp: input schema: %w", err)
	}
	if refine != nil {
		refine(s)
	}
	return s, nil
}

func bound(s *jsonschema.Schema, lo, hi int) {
	l, h := float64(lo), float64(hi)
	s.Minimum, s.Maximum = &l, &h
}

func ptr[T any](v T) *T { return &v }
