// Package search keeps the Meilisearch index in step with Postgres and serves
// access-filtered queries (docs/planning/05-search-and-indexing.md).
//
// The browser never talks to Meilisearch: the API holds the key, injects the
// access filter server-side, and returns sanitized hits. Postgres stays the
// source of truth; the index is a rebuildable projection fed by the
// index_outbox worker.
package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// IndexName is the single Meilisearch index holding published documents.
const IndexName = "documents"

// Highlight markers. Control characters cannot appear in rendered text, so after
// HTML-escaping a snippet they are the only thing turned into <mark> tags.
const (
	markPre  = "\x01"
	markPost = "\x02"
)

// errIndexNotFound is returned by stats when the index does not exist yet.
var errIndexNotFound = errors.New("meilisearch index not found")

// Client is a minimal Meilisearch HTTP client covering what LookupKB needs.
type Client struct {
	base string
	key  string
	http *http.Client
}

// NewClient builds a client for the Meilisearch base URL and API key.
func NewClient(baseURL, key string) *Client {
	return &Client{
		base: strings.TrimRight(baseURL, "/"),
		key:  key,
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

// IndexDoc is the projection stored for each published document.
type IndexDoc struct {
	ID          string   `json:"id"`
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	BodyText    string   `json:"body_text"`
	Tags        []string `json:"tags"`
	Author      string   `json:"author"`
	AuthorID    string   `json:"author_id"`
	CreatedAt   int64    `json:"created_at"`
	PublishedAt int64    `json:"published_at"`
	Access      string   `json:"access"`
	ACLGroupIDs []string `json:"acl_group_ids"`
}

type taskRef struct {
	TaskUID int64 `json:"taskUid"`
}

// EnsureIndex creates the index (if missing) and applies its settings, waiting
// for the settings task so filters are usable before the first query.
func (c *Client) EnsureIndex(ctx context.Context) error {
	var created taskRef
	if err := c.do(ctx, http.MethodPost, "/indexes", map[string]string{"uid": IndexName, "primaryKey": "id"}, &created); err != nil {
		return fmt.Errorf("create index: %w", err)
	}
	// Creation fails asynchronously when the index already exists; that's fine.
	_ = c.waitTask(ctx, created.TaskUID)

	settings := map[string]any{
		"searchableAttributes": []string{"title", "description", "tags", "body_text"},
		"filterableAttributes": []string{"tags", "author_id", "access", "acl_group_ids", "created_at", "published_at"},
		"sortableAttributes":   []string{"published_at", "created_at"},
		"displayedAttributes":  []string{"id", "slug", "title", "description", "tags", "author", "author_id", "published_at", "body_text"},
		"typoTolerance": map[string]any{
			"enabled":             true,
			"minWordSizeForTypos": map[string]int{"oneTypo": 4, "twoTypos": 8},
		},
	}
	var t taskRef
	if err := c.do(ctx, http.MethodPatch, "/indexes/"+IndexName+"/settings", settings, &t); err != nil {
		return fmt.Errorf("update settings: %w", err)
	}
	return c.waitTask(ctx, t.TaskUID)
}

// Upsert adds or replaces documents in the index.
func (c *Client) Upsert(ctx context.Context, docs ...IndexDoc) error {
	return c.do(ctx, http.MethodPost, "/indexes/"+IndexName+"/documents", docs, nil)
}

// Delete removes one document from the index (a missing id is not an error).
func (c *Client) Delete(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/indexes/"+IndexName+"/documents/"+url.PathEscape(id), nil, nil)
}

// DocumentCount reports how many documents the index holds.
func (c *Client) DocumentCount(ctx context.Context) (int64, error) {
	var stats struct {
		NumberOfDocuments int64 `json:"numberOfDocuments"`
	}
	if err := c.do(ctx, http.MethodGet, "/indexes/"+IndexName+"/stats", nil, &stats); err != nil {
		return 0, err
	}
	return stats.NumberOfDocuments, nil
}

type searchRequest struct {
	Q                     string   `json:"q"`
	Filter                string   `json:"filter,omitempty"`
	Limit                 int      `json:"limit"`
	Sort                  []string `json:"sort,omitempty"`
	AttributesToRetrieve  []string `json:"attributesToRetrieve"`
	AttributesToHighlight []string `json:"attributesToHighlight"`
	AttributesToCrop      []string `json:"attributesToCrop"`
	CropLength            int      `json:"cropLength"`
	HighlightPreTag       string   `json:"highlightPreTag"`
	HighlightPostTag      string   `json:"highlightPostTag"`
}

type rawHit struct {
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Author      string   `json:"author"`
	PublishedAt int64    `json:"published_at"`
	Formatted   struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		BodyText    string `json:"body_text"`
	} `json:"_formatted"`
}

type searchResponse struct {
	Hits             []rawHit `json:"hits"`
	ProcessingTimeMs int64    `json:"processingTimeMs"`
}

func (c *Client) search(ctx context.Context, req searchRequest) (searchResponse, error) {
	var resp searchResponse
	err := c.do(ctx, http.MethodPost, "/indexes/"+IndexName+"/search", req, &resp)
	return resp, err
}

func (c *Client) waitTask(ctx context.Context, uid int64) error {
	deadline := time.Now().Add(30 * time.Second)
	for {
		var task struct {
			Status string `json:"status"`
			Error  *struct {
				Message string `json:"message"`
				Code    string `json:"code"`
			} `json:"error"`
		}
		if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/tasks/%d", uid), nil, &task); err != nil {
			return err
		}
		switch task.Status {
		case "succeeded":
			return nil
		case "failed", "canceled":
			if task.Error != nil {
				return fmt.Errorf("meilisearch task %d %s: %s (%s)", uid, task.Status, task.Error.Message, task.Error.Code)
			}
			return fmt.Errorf("meilisearch task %d %s", uid, task.Status)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("meilisearch task %d still %s", uid, task.Status)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, r)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound && strings.HasSuffix(path, "/stats") {
		return errIndexNotFound
	}
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("meilisearch %s %s: %d %s", method, path, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
