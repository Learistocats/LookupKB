// Package render is the security-critical publish-time Markdown pipeline
// (01-architecture.md §4, 06-security.md §2). Author-authored Markdown is
// untrusted content consumed by many readers, so it is rendered to HTML and
// sanitized ONCE, server-side, at publish time, then cached in
// documents.rendered_html — never re-parsed on the read path.
//
// The single entry point is Renderer.RenderAndSanitize, which the publish gate
// (LOO-14) calls. The flow is deterministic: size-cap check → goldmark render
// (GFM + footnotes, raw HTML passed through) → bluemonday allowlist sanitize.
// goldmark is NOT a sanitizer, so bluemonday (see policy.go) is the single
// authoritative gate that strips scripts, event handlers and dangerous URLs.
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
// The publish gate injects the admin-tunable limit via WithMaxBytes; this default
// keeps a directly-constructed Renderer safe. See 06-security.md §6 (DoS row).
const DefaultMaxBytes = 1 << 20

// ErrSourceTooLarge is returned when the Markdown source exceeds the configured
// size cap. It is rejected outright (never truncated), and callers (LOO-14) map
// it to a 422 field error. errors.Is against it to detect the condition.
var ErrSourceTooLarge = errors.New("markdown source exceeds size limit")

// Renderer holds the configured goldmark markdown engine and bluemonday policy.
// It is safe for concurrent use (both underlying values are read-only after
// construction) so a single Renderer is shared process-wide. Construct with New.
type Renderer struct {
	md       goldmark.Markdown
	policy   *bluemonday.Policy
	maxBytes int
	diagrams DiagramRenderer
}

// Option configures a Renderer at construction.
type Option func(*Renderer)

// WithMaxBytes sets the Markdown source size cap. Non-positive values are ignored
// (the default is kept), so a mis-set env var can never disable the bound.
func WithMaxBytes(n int) Option {
	return func(r *Renderer) {
		if n > 0 {
			r.maxBytes = n
		}
	}
}

// WithDiagramRenderer supplies the diagram engine (Kroki, LOO-19). When omitted
// the Renderer uses NoopDiagramRenderer, leaving fenced diagram blocks as code.
func WithDiagramRenderer(d DiagramRenderer) Option {
	return func(r *Renderer) {
		if d != nil {
			r.diagrams = d
		}
	}
}

// New constructs a Renderer with the GFM + footnote goldmark extensions and the
// strict bluemonday policy. html.WithUnsafe() lets goldmark emit raw HTML so the
// bluemonday policy is the single authoritative sanitizer (goldmark is not one).
func New(opts ...Option) *Renderer {
	// Table alignment is rendered as an `align` attribute rather than goldmark's
	// default inline `style="text-align:…"` — the policy strips style="" (an XSS/
	// exfiltration vector), so without this GFM column alignment would be lost.
	table := extension.NewTable(
		extension.WithTableCellAlignMethod(extension.TableCellAlignAttribute),
	)

	md := goldmark.New(
		// The must-have GFM features from 00-overview.md §7: tables, strikethrough,
		// linkify (autolinks), task lists, plus footnotes. Listed individually
		// (rather than the extension.GFM bundle) so the table uses the align-attr
		// renderer above.
		goldmark.WithExtensions(
			table,
			extension.Strikethrough,
			extension.Linkify,
			extension.TaskList,
			extension.Footnote,
		),
		goldmark.WithRendererOptions(
			// Emit raw HTML rather than escaping it, so bluemonday — not goldmark —
			// decides what survives. Without this, goldmark would silently escape
			// author HTML and the sanitizer's allowlist would never be exercised.
			html.WithUnsafe(),
		),
	)

	r := &Renderer{
		md:       md,
		policy:   newPolicy(),
		maxBytes: DefaultMaxBytes,
		diagrams: NoopDiagramRenderer{},
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// DiagramRenderer returns the configured diagram engine (NoopDiagramRenderer by
// default). Exposed so LOO-19's wiring and tests can assert what is installed.
func (r *Renderer) DiagramRenderer() DiagramRenderer { return r.diagrams }

// RenderAndSanitize renders Markdown source to sanitized HTML safe to persist in
// documents.rendered_html and serve to readers. It is the sole producer of that
// column's value (06-security.md §2) and must be invoked only at publish time,
// never on the read path.
//
// Order is load-bearing: the size cap is checked BEFORE any parsing so an
// oversized document cannot drive render cost, and sanitization runs on the FINAL
// HTML string that will actually be stored (goldmark output is never trusted).
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
