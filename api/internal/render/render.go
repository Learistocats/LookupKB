// Package render is the publish-time Markdown pipeline. Author Markdown is
// untrusted, so it is rendered and sanitized once at publish and cached in
// documents.rendered_html; the read path never re-parses it.
//
// goldmark is not a sanitizer: bluemonday (policy.go) is the single gate that
// strips scripts, event handlers and dangerous URLs.
package render

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

// DefaultMaxBytes bounds Markdown source size when no cap is configured (1 MiB).
const DefaultMaxBytes = 1 << 20

// ErrSourceTooLarge is returned when the source exceeds the size cap; it is
// rejected, never truncated.
var ErrSourceTooLarge = errors.New("markdown source exceeds size limit")

// Renderer is safe for concurrent use; share one process-wide. Construct with New.
type Renderer struct {
	md       goldmark.Markdown
	policy   *bluemonday.Policy
	maxBytes int
}

// Option configures a Renderer at construction.
type Option func(*Renderer)

// WithMaxBytes sets the source size cap. Non-positive values are ignored so a
// mis-set value can never disable the bound.
func WithMaxBytes(n int) Option {
	return func(r *Renderer) {
		if n > 0 {
			r.maxBytes = n
		}
	}
}

// New constructs a Renderer with GFM + footnotes and the strict bluemonday policy.
func New(opts ...Option) *Renderer {
	// Use the align attribute: the policy strips style="", which would lose
	// goldmark's default inline text-align.
	table := extension.NewTable(
		extension.WithTableCellAlignMethod(extension.TableCellAlignAttribute),
	)

	md := goldmark.New(
		// Listed individually (not extension.GFM) to use the align-attr table.
		goldmark.WithExtensions(
			table,
			extension.Strikethrough,
			extension.Linkify,
			extension.TaskList,
			extension.Footnote,
		),
		goldmark.WithRendererOptions(
			// Emit raw HTML so bluemonday, not goldmark, decides what survives.
			html.WithUnsafe(),
		),
	)

	r := &Renderer{
		md:       md,
		policy:   newPolicy(),
		maxBytes: DefaultMaxBytes,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// RenderAndSanitize renders Markdown to sanitized HTML for documents.rendered_html.
// Call only at publish time. The size cap is checked before parsing so an
// oversized document cannot drive render cost; sanitization runs on the final HTML.
func (r *Renderer) RenderAndSanitize(_ context.Context, sourceMD string) (string, error) {
	if len(sourceMD) > r.maxBytes {
		return "", fmt.Errorf("%w: %d bytes (limit %d)", ErrSourceTooLarge, len(sourceMD), r.maxBytes)
	}

	var buf bytes.Buffer
	if err := r.md.Convert([]byte(sourceMD), &buf); err != nil {
		return "", fmt.Errorf("render markdown: %w", err)
	}

	safe := r.policy.SanitizeBytes(buf.Bytes())
	return string(safe), nil
}
