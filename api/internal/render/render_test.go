package render

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRenderAndSanitize_XSSCorpus is the SEC-1 seed (06-security.md §10,
// 07-test-book.md SEC-1): a battery of malicious Markdown/HTML/SVG payloads must
// be neutralised. For each payload the sanitized output must contain none of the
// dangerous tokens. This is the guarantee that documents.rendered_html is safe to
// serve to every reader.
func TestRenderAndSanitize_XSSCorpus(t *testing.T) {
	r := New()

	// Tokens that must never survive in any output (checked case-insensitively).
	forbidden := []string{
		"<script", "javascript:", "data:text/html", "<iframe", "<object",
		"<embed", "<style", "onerror=", "onclick=", "onload=", "onmouseover=",
	}

	cases := map[string]string{
		"raw script tag":           `<script>alert(1)</script>`,
		"img onerror handler":      `<img src=x onerror=alert(1)>`,
		"anchor javascript scheme": `<a href="javascript:alert(1)">x</a>`,
		"markdown link javascript": `[click](javascript:alert(1))`,
		"div onclick handler":      `<div onclick="alert(1)">hi</div>`,
		"iframe injection":         `<iframe src="https://evil.example"></iframe>`,
		"object injection":         `<object data="evil.swf"></object>`,
		"embed injection":          `<embed src="evil.swf">`,
		"style tag":                `<style>body{background:url(javascript:alert(1))}</style>`,
		"data uri html link":       `<a href="data:text/html,<script>alert(1)</script>">x</a>`,
		"markdown img data uri":    `![x](data:text/html,<script>alert(1)</script>)`,
		"nested malformed script":  `<scr<script>ipt>alert(1)</script>`,
		"svg with script":          `<svg><script>alert(1)</script></svg>`,
		"body onload":              `<body onload=alert(1)>`,
		"a onmouseover":            `<a href="https://ok.example" onmouseover="alert(1)">x</a>`,
		"svg animate xss":          `<svg><a href="javascript:alert(1)"><text>x</text></a></svg>`,
	}

	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := r.RenderAndSanitize(context.Background(), src)
			require.NoError(t, err)
			low := strings.ToLower(out)
			for _, tok := range forbidden {
				require.NotContainsf(t, low, tok, "payload %q leaked %q in output: %s", src, tok, out)
			}
		})
	}
}

// TestRenderAndSanitize_GFMPreserved asserts valid GFM keeps its intended
// structure/semantics — no false-positive stripping (ticket AC). Every must-have
// GFM feature from 00-overview.md §7 is exercised.
func TestRenderAndSanitize_GFMPreserved(t *testing.T) {
	r := New()

	t.Run("table", func(t *testing.T) {
		out := mustRender(t, r, "| a | b |\n|:--|--:|\n| 1 | 2 |\n")
		require.Contains(t, out, "<table>")
		require.Contains(t, out, "<th")
		require.Contains(t, out, "<td")
		require.Contains(t, out, `align="left"`)
		require.Contains(t, out, `align="right"`)
	})

	t.Run("task list", func(t *testing.T) {
		out := mustRender(t, r, "- [x] done\n- [ ] todo\n")
		require.Contains(t, out, `type="checkbox"`)
		require.Contains(t, out, "disabled")
		require.Contains(t, out, "checked")
	})

	t.Run("strikethrough", func(t *testing.T) {
		out := mustRender(t, r, "~~gone~~")
		require.Contains(t, out, "<del>")
	})

	t.Run("autolink", func(t *testing.T) {
		out := mustRender(t, r, "see https://example.com now")
		require.Contains(t, out, `href="https://example.com"`)
	})

	t.Run("footnote", func(t *testing.T) {
		out := mustRender(t, r, "text[^1]\n\n[^1]: the note\n")
		require.Contains(t, out, "<sup")
		require.Contains(t, out, `href="#`)  // fragment ref preserved
		require.Contains(t, out, "the note") // note body preserved
	})

	t.Run("fenced code keeps language class", func(t *testing.T) {
		out := mustRender(t, r, "```go\nfmt.Println(\"hi\")\n```\n")
		require.Contains(t, out, "<pre>")
		require.Contains(t, out, `class="language-go"`)
	})

	t.Run("basic formatting", func(t *testing.T) {
		out := mustRender(t, r, "# Title\n\n**bold** and *em* and [link](https://ok.example)\n")
		require.Contains(t, out, "<h1")
		require.Contains(t, out, "<strong>")
		require.Contains(t, out, "<em>")
		require.Contains(t, out, `href="https://ok.example"`)
	})
}

// TestRenderAndSanitize_SizeCap rejects oversized source before rendering, with a
// clear typed error — never silently truncated (ticket AC, 06-security.md §6).
func TestRenderAndSanitize_SizeCap(t *testing.T) {
	r := New(WithMaxBytes(16))

	out, err := r.RenderAndSanitize(context.Background(), strings.Repeat("a", 17))
	require.ErrorIs(t, err, ErrSourceTooLarge)
	require.Empty(t, out)

	// At the limit it renders normally (boundary is inclusive).
	out, err = r.RenderAndSanitize(context.Background(), strings.Repeat("a", 16))
	require.NoError(t, err)
	require.Contains(t, out, "aaaa")
}

// TestWithMaxBytes_IgnoresNonPositive ensures a mis-set limit cannot disable the
// bound: a zero/negative value keeps the safe default.
func TestWithMaxBytes_IgnoresNonPositive(t *testing.T) {
	r := New(WithMaxBytes(0))
	require.Equal(t, DefaultMaxBytes, r.maxBytes)
}

// TestNew_DefaultDiagramRenderer confirms the M1 default is the no-op renderer,
// which emits no SVG so fenced diagram blocks stay as plain code (LOO-19 swaps it).
func TestNew_DefaultDiagramRenderer(t *testing.T) {
	r := New()
	require.IsType(t, NoopDiagramRenderer{}, r.DiagramRenderer())

	svg, err := r.DiagramRenderer().Render(context.Background(), "mermaid", "graph TD; A-->B")
	require.NoError(t, err)
	require.Empty(t, svg)
}

func mustRender(t *testing.T, r *Renderer, src string) string {
	t.Helper()
	out, err := r.RenderAndSanitize(context.Background(), src)
	require.NoError(t, err)
	return out
}

// Guard: ErrSourceTooLarge must remain a distinct sentinel for errors.Is.
func TestErrSourceTooLarge_IsSentinel(t *testing.T) {
	require.True(t, errors.Is(ErrSourceTooLarge, ErrSourceTooLarge))
}
