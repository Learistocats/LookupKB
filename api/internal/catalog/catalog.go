// Package catalog is the read side of the published corpus for programmatic
// callers (LOO-38 public read API, LOO-59 MCP tools): list the published entries
// a caller may read, and fetch one in a chosen representation.
//
// Every decision goes through the protected-tag policy (internal/access): the list
// excludes denied entries in SQL BEFORE pagination, and a single read answers
// ErrNotFound for a missing, unpublished and denied slug alike, so a caller can
// never tell "doesn't exist" from "exists but hidden".
//
// Representations are straight column reads of what the publish transaction
// stored — rendered_html (sanitized once at publish) and published_md (the source
// it was rendered from). Nothing here parses or sanitizes Markdown, and the
// working copy (source_md) is never read: it may hold unpublished edits.
package catalog

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Learistocats/LookupKB/api/internal/access"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/Learistocats/LookupKB/api/internal/search"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Page-size bounds for List.
const (
	DefaultLimit = 20
	MaxLimit     = 100
)

var (
	// ErrNotFound is returned for a slug that is missing, not published, deleted
	// or not readable by the caller — deliberately one error for all of them.
	ErrNotFound = errors.New("document not found")
	// ErrInvalidCursor marks a malformed pagination cursor (a client error).
	ErrInvalidCursor = errors.New("invalid cursor")
	// ErrIntegrity marks a published row missing its publish-time content; it is
	// a data bug to surface (500), never something to recompute on the read path.
	ErrIntegrity = errors.New("published document is missing its published content")
)

// Store is the data access the catalog needs. *dbgen.Queries satisfies it.
type Store interface {
	GetPublishedBySlug(ctx context.Context, slug string) (dbgen.GetPublishedBySlugRow, error)
	ListPublishedDocuments(ctx context.Context, arg dbgen.ListPublishedDocumentsParams) ([]dbgen.ListPublishedDocumentsRow, error)
	access.Querier
}

// Service answers catalog reads for a verified caller.
type Service struct {
	store Store
}

// New builds the catalog over the store.
func New(store Store) *Service { return &Service{store: store} }

// Summary is one entry's public metadata (no body).
type Summary struct {
	Slug              string   `json:"slug"`
	Title             string   `json:"title"`
	Description       string   `json:"description"`
	Tags              []string `json:"tags"`
	AuthorDisplayName string   `json:"author_display_name"`
	PublishedAt       string   `json:"published_at"`
	UpdatedAt         string   `json:"updated_at"`
}

// Page is one List result. NextCursor is empty on the last page.
type Page struct {
	Documents  []Summary `json:"documents"`
	NextCursor string    `json:"next_cursor,omitempty"`
}

// Document is one readable published entry with its stored representations.
type Document struct {
	Summary
	// ID and AuthorID are internal: never serialized to a reader (the internal
	// read view reveals ID to the author only).
	ID       pgtype.UUID `json:"-"`
	AuthorID pgtype.UUID `json:"-"`
	HTML     string      `json:"-"`
	// Markdown is published_md (nil only on a data-integrity bug).
	Markdown *string `json:"-"`
}

// Get returns the published entry at slug if the caller may read it. The policy
// is resolved before the lookup on every call, so a missing and a denied slug
// cost the same work (no timing oracle).
func (s *Service) Get(ctx context.Context, caller access.Caller, slug string) (Document, error) {
	if slug == "" {
		return Document{}, ErrNotFound
	}
	policy, err := access.Load(ctx, s.store, caller)
	if err != nil {
		return Document{}, err
	}
	row, err := s.store.GetPublishedBySlug(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, ErrNotFound
	}
	if err != nil {
		return Document{}, fmt.Errorf("catalog: get document: %w", err)
	}
	if !policy.CanRead(row.AuthorID, row.TagSlugs) {
		return Document{}, ErrNotFound
	}
	if row.RenderedHtml == nil {
		return Document{}, ErrIntegrity
	}
	return Document{
		Summary: Summary{
			Slug:              row.Slug,
			Title:             deref(row.Title),
			Description:       deref(row.Description),
			Tags:              nonNil(row.Tags),
			AuthorDisplayName: row.AuthorDisplayName,
			PublishedAt:       formatTime(row.PublishedAt),
			UpdatedAt:         formatTime(row.UpdatedAt),
		},
		ID:       row.ID,
		AuthorID: row.AuthorID,
		HTML:     *row.RenderedHtml,
		Markdown: row.PublishedMd,
	}, nil
}

// List returns one page of the published entries the caller may read, newest
// first. limit <= 0 means DefaultLimit; it is capped at MaxLimit. cursor is the
// previous page's NextCursor ("" for the first page).
func (s *Service) List(ctx context.Context, caller access.Caller, limit int, cursor string) (Page, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	at, id, err := decodeCursor(cursor)
	if err != nil {
		return Page{}, err
	}
	policy, err := access.Load(ctx, s.store, caller)
	if err != nil {
		return Page{}, err
	}
	rows, err := s.store.ListPublishedDocuments(ctx, dbgen.ListPublishedDocumentsParams{
		CallerID:          caller.UserID,
		DeniedIds:         policy.DeniedIDs(),
		CursorPublishedAt: at,
		CursorID:          id,
		// One extra row says whether a next page exists; it is never returned.
		MaxRows: int32(limit + 1), //nolint:gosec // limit is bounded by MaxLimit
	})
	if err != nil {
		return Page{}, fmt.Errorf("catalog: list documents: %w", err)
	}
	page := Page{Documents: make([]Summary, 0, min(len(rows), limit))}
	for i, r := range rows {
		if i == limit {
			last := rows[limit-1]
			page.NextCursor = encodeCursor(last.PublishedAt, last.ID)
			break
		}
		page.Documents = append(page.Documents, Summary{
			Slug:              r.Slug,
			Title:             deref(r.Title),
			Description:       deref(r.Description),
			Tags:              nonNil(r.Tags),
			AuthorDisplayName: r.AuthorDisplayName,
			PublishedAt:       formatTime(r.PublishedAt),
			UpdatedAt:         formatTime(r.UpdatedAt),
		})
	}
	return page, nil
}

// Format is a document representation.
type Format string

// The representations a document can be served in.
const (
	FormatJSON     Format = "json"
	FormatHTML     Format = "html"
	FormatText     Format = "text"
	FormatMarkdown Format = "markdown"
)

// ParseFormat validates a ?format= value.
func ParseFormat(s string) (Format, bool) {
	switch f := Format(strings.ToLower(strings.TrimSpace(s))); f {
	case FormatJSON, FormatHTML, FormatText, FormatMarkdown:
		return f, true
	}
	return "", false
}

// Body returns the document in a text representation:
//   - html: the cached, publish-time sanitized rendered_html, verbatim;
//   - text: plain text extracted from that HTML (the search indexer's walk,
//     keeping line structure — search.ReadableTextFromHTML);
//   - markdown: published_md, the source rendered_html was rendered from.
func (d Document) Body(f Format) (string, error) {
	switch f {
	case FormatHTML:
		return d.HTML, nil
	case FormatText:
		return search.ReadableTextFromHTML(d.HTML), nil
	case FormatMarkdown:
		if d.Markdown == nil {
			return "", ErrIntegrity
		}
		return *d.Markdown, nil
	}
	return "", fmt.Errorf("catalog: format %q has no text body", f)
}

// cursor = base64url("<published_at unix µs>.<uuid>"): opaque to clients, exact to
// Postgres' microsecond timestamps.
func encodeCursor(at pgtype.Timestamptz, id pgtype.UUID) string {
	raw := strconv.FormatInt(at.Time.UnixMicro(), 10) + "." + uuid.UUID(id.Bytes).String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(c string) (pgtype.Timestamptz, pgtype.UUID, error) {
	if c == "" {
		return pgtype.Timestamptz{}, pgtype.UUID{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, ErrInvalidCursor
	}
	ts, idText, ok := strings.Cut(string(raw), ".")
	if !ok {
		return pgtype.Timestamptz{}, pgtype.UUID{}, ErrInvalidCursor
	}
	micros, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, ErrInvalidCursor
	}
	id, err := uuid.Parse(idText)
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, ErrInvalidCursor
	}
	return pgtype.Timestamptz{Time: time.UnixMicro(micros).UTC(), Valid: true}, pgtype.UUID{Bytes: id, Valid: true}, nil
}

func formatTime(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.UTC().Format(time.RFC3339)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
