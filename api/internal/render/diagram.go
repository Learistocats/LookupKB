package render

import "context"

// DiagramRenderer turns a fenced diagram block (```mermaid, ```plantuml, …) into
// inlined SVG at publish time. It is the clean extension point 01-architecture.md
// §4 calls for: the heavy, historically XSS-prone diagram engines run server-side
// in Kroki (LOO-19), never in the reader's browser, and their SVG is inlined once
// and sanitized.
//
// LOO-16 defines the seam only; LOO-19 supplies the Kroki implementation. Keeping
// the interface here now means M4 wires in a real renderer (and the SVG-specific
// sanitization it requires — see policy.go) without reshaping the pipeline.
type DiagramRenderer interface {
	// Render returns the SVG for a diagram of the given fenced language (e.g.
	// "mermaid", "plantuml"). Implementations MUST return already-safe SVG; the
	// pipeline does not re-sanitize their output element-by-element beyond the
	// structural policy. lang is the fenced info string; source is the block body.
	Render(ctx context.Context, lang, source string) (svg string, err error)
}

// NoopDiagramRenderer is the M1 default: it renders nothing, leaving fenced
// diagram blocks as the plain <pre><code> goldmark already produced. This keeps
// M1 free of a Kroki dependency while preserving the block's source so LOO-19 can
// light it up. It never errors and never emits SVG.
type NoopDiagramRenderer struct{}

// Render reports that no SVG was produced (empty string), signalling the pipeline
// to leave the fenced block as-is. It is deliberately a no-op for M1.
func (NoopDiagramRenderer) Render(context.Context, string, string) (string, error) {
	return "", nil
}
