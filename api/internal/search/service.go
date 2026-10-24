package search

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Learistocats/LookupKB/api/internal/access"
	"github.com/Learistocats/LookupKB/api/internal/db"
)

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

// Result is a page of hits. Facets is present only when requested
// (?facets=tags): facet value → count, computed by Meilisearch under the same
// access filter as the hits, so a count never reveals an entry the caller cannot
// open.
type Result struct {
	Hits   []Hit                       `json:"hits"`
	TookMs int64                       `json:"took_ms"`
	Facets map[string]map[string]int64 `json:"facets,omitempty"`
	// NextOffset is the offset of the next page, present only when there is one.
	NextOffset *int `json:"next_offset,omitempty"`
}

// Query is one search request: free text, page size, client filters and the
// facets to compute.
type Query struct {
	Q     string
	Limit int
	// Offset skips that many hits (pagination); 0..MaxOffset.
	Offset  int
	Filters Filters
	// Facets to compute; only FacetTags is allowed.
	Facets []string
}

// Service runs access-filtered queries against the index. It reads the caller's
// protected-tag policy from Postgres on every query (internal/access).
type Service struct {
	client *Client
	policy access.Querier
}

// NewService builds the query service over a Meilisearch client and the access
// policy source (*dbgen.Queries).
func NewService(c *Client, policy access.Querier) *Service {
	return &Service{client: c, policy: policy}
}

// Page-size bounds: a missing limit means DefaultLimit, a larger one is clamped.
const (
	DefaultLimit = 20
	MaxLimit     = 50
)

// MaxQueryRunes bounds the free-text query forwarded to the engine; longer
// queries are truncated.
const MaxQueryRunes = 200

// MaxOffset caps pagination depth. Meilisearch's default maxTotalHits is 1000,
// so hits past it are unreachable anyway; a deeper offset is refused.
const MaxOffset = 1000

// Search returns hits the caller may open. An empty query lists the most
// recently published documents. A refused filter/facet wraps ErrInvalidFilter.
func (s *Service) Search(ctx context.Context, query Query, caller access.Caller) (Result, error) {
	facets, err := allowedFacets(query.Facets)
	if err != nil {
		return Result{}, err
	}
	policy, err := access.Load(ctx, s.policy, caller)
	if err != nil {
		return Result{}, err
	}
	filter, err := buildFilter(policy, query.Filters)
	if err != nil {
		return Result{}, err
	}
	limit := query.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	limit = min(limit, MaxLimit)
	if query.Offset < 0 || query.Offset > MaxOffset {
		return Result{}, fmt.Errorf("%w: offset out of range", ErrInvalidFilter)
	}
	req := searchRequest{
		Q:      truncateRunes(strings.TrimSpace(query.Q), MaxQueryRunes),
		Filter: filter,
		Facets: facets,
		// One extra hit tells whether a next page exists; it is never returned.
		Limit:                 limit + 1,
		Offset:                query.Offset,
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
	if len(resp.Hits) > limit {
		resp.Hits = resp.Hits[:limit]
		if next := query.Offset + limit; next <= MaxOffset {
			out.NextOffset = &next
		}
	}
	if len(facets) > 0 {
		out.Facets = make(map[string]map[string]int64, len(facets))
		for _, f := range facets {
			dist := resp.FacetDistribution[f]
			if dist == nil {
				dist = map[string]int64{}
			}
			out.Facets[f] = dist
		}
	}
	for _, h := range resp.Hits {
		out.Hits = append(out.Hits, Hit{
			Slug:        h.Slug,
			Title:       h.Title,
			TitleHTML:   markHTML(orDefault(h.Formatted.Title, h.Title)),
			Description: h.Description,
			Snippet:     markHTML(pickSnippet(h, req.Q != "")),
			Tags:        db.NonNil(h.Tags),
			Author:      h.Author,
			PublishedAt: time.Unix(h.PublishedAt, 0).UTC().Format(time.RFC3339),
		})
	}
	return out, nil
}

func truncateRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
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

// allowedFacets validates the requested facets against the allowlist.
func allowedFacets(in []string) ([]string, error) {
	var out []string
	for _, f := range in {
		f = strings.TrimSpace(f)
		switch f {
		case "":
			continue
		case FacetTags:
			if len(out) == 0 {
				out = append(out, f)
			}
		default:
			return nil, fmt.Errorf("%w: facet %q", ErrInvalidFilter, f)
		}
	}
	return out, nil
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
