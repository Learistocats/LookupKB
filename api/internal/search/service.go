package search

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Caller identifies who is searching; it drives the access filter.
type Caller struct {
	UserID string
	Groups []string
}

// Hit is one sanitized search result. TitleHTML and Snippet contain only escaped
// text plus <mark> elements, so the UI may render them as HTML.
type Hit struct {
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	TitleHTML   string   `json:"title_html"`
	Description string   `json:"description"`
	Snippet     string   `json:"snippet"`
	Tags        []string `json:"tags"`
	Author      string   `json:"author"`
	PublishedAt string   `json:"published_at"`
}

// Result is a page of hits.
type Result struct {
	Hits   []Hit `json:"hits"`
	TookMs int64 `json:"took_ms"`
}

// Service runs access-filtered queries against the index.
type Service struct {
	client *Client
}

// NewService builds the query service over a Meilisearch client.
func NewService(c *Client) *Service { return &Service{client: c} }

// MaxLimit caps how many hits one query may return.
const MaxLimit = 50

// Search returns hits the caller may open. An empty query lists the most
// recently published documents.
func (s *Service) Search(ctx context.Context, q string, limit int, caller Caller) (Result, error) {
	filter, err := AccessFilter(caller)
	if err != nil {
		return Result{}, err
	}
	if limit <= 0 || limit > MaxLimit {
		limit = 20
	}
	req := searchRequest{
		Q:                     strings.TrimSpace(q),
		Filter:                filter,
		Limit:                 limit,
		AttributesToRetrieve:  []string{"slug", "title", "description", "tags", "author", "published_at"},
		AttributesToHighlight: []string{"title", "description", "body_text"},
		AttributesToCrop:      []string{"body_text", "description"},
		CropLength:            28,
		HighlightPreTag:       markPre,
		HighlightPostTag:      markPost,
	}
	if req.Q == "" {
		req.Sort = []string{"published_at:desc"}
	}
	resp, err := s.client.search(ctx, req)
	if err != nil {
		return Result{}, err
	}
	out := Result{Hits: make([]Hit, 0, len(resp.Hits)), TookMs: resp.ProcessingTimeMs}
	for _, h := range resp.Hits {
		tags := h.Tags
		if tags == nil {
			tags = []string{}
		}
		out.Hits = append(out.Hits, Hit{
			Slug:        h.Slug,
			Title:       h.Title,
			TitleHTML:   markHTML(orDefault(h.Formatted.Title, h.Title)),
			Description: h.Description,
			Snippet:     markHTML(pickSnippet(h, req.Q != "")),
			Tags:        tags,
			Author:      h.Author,
			PublishedAt: time.Unix(h.PublishedAt, 0).UTC().Format(time.RFC3339),
		})
	}
	return out, nil
}

// pickSnippet prefers a body excerpt that contains a match, then the
// description.
func pickSnippet(h rawHit, hasQuery bool) string {
	body := h.Formatted.BodyText
	if hasQuery && strings.Contains(body, markPre) {
		return body
	}
	if d := h.Formatted.Description; d != "" {
		return d
	}
	return body
}

// AccessFilter builds the Meilisearch filter a caller's queries always carry:
// public documents, the caller's own, and restricted ones shared with one of the
// caller's groups. Ids are validated as UUIDs so nothing user-controlled is ever
// spliced into the filter expression.
func AccessFilter(c Caller) (string, error) {
	uid, err := uuid.Parse(c.UserID)
	if err != nil {
		return "", fmt.Errorf("invalid caller id: %w", err)
	}
	parts := []string{`access = "public"`, `author_id = ` + strconv.Quote(uid.String())}
	if len(c.Groups) > 0 {
		quoted := make([]string, 0, len(c.Groups))
		for _, g := range c.Groups {
			gid, err := uuid.Parse(g)
			if err != nil {
				return "", fmt.Errorf("invalid group id: %w", err)
			}
			quoted = append(quoted, strconv.Quote(gid.String()))
		}
		parts = append(parts, `acl_group_ids IN [`+strings.Join(quoted, ", ")+`]`)
	}
	return strings.Join(parts, " OR "), nil
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
