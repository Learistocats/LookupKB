package render

import (
	"regexp"

	"github.com/microcosm-cc/bluemonday"
)

// newPolicy builds the strict allowlist bluemonday policy applied to every
// publish-time HTML render (06-security.md §2 XSS row). It is deliberately built
// from an empty NewPolicy() rather than UGCPolicy(): every tag and attribute
// below is an explicit, reviewable decision. Security reviewers should be able to
// read the entire allowed surface in this one function.
//
// Invariants enforced here:
//   - No <script>, no event-handler attributes (on*), no <style>/style="" — none
//     are allowed, so none can survive.
//   - href/src are restricted to an http/https/mailto scheme allowlist plus
//     fragment/relative refs; javascript: and data: URLs are rejected. Footnote
//     and autolink (GFM) extensions inject href, so this is load-bearing.
//   - class/id/role are permitted only through conservative regexes, never open.
func newPolicy() *bluemonday.Policy {
	p := bluemonday.NewPolicy()

	// --- Links: scheme-restricted, no-follow, fragment/relative allowed ---------
	// AllowStandardURLs wires the safe defaults (no javascript:, parseable URLs,
	// rel="nofollow"); we then pin the scheme allowlist explicitly. Relative and
	// fragment URLs (e.g. footnote "#fn:1") are permitted — they carry no scheme
	// and cannot execute.
	p.AllowStandardURLs()
	p.AllowURLSchemes("http", "https", "mailto")
	p.AllowRelativeURLs(true)
	p.RequireNoFollowOnLinks(true)
	p.AllowAttrs("href", "rel").OnElements("a")
	p.AllowAttrs("target").Matching(regexp.MustCompile(`^_blank$`)).OnElements("a")

	// --- Block & inline text structure -----------------------------------------
	p.AllowElements(
		"h1", "h2", "h3", "h4", "h5", "h6",
		"p", "br", "hr", "blockquote",
		"ul", "ol", "li",
		"strong", "em", "del", "s", "b", "i", "u",
		"sup", "sub", "span", "div", "a",
	)

	// --- Code blocks ------------------------------------------------------------
	// goldmark emits <pre><code class="language-go"> for fenced blocks; allow the
	// language-* class so syntax-highlighting metadata survives. Nothing inside a
	// code block is trusted as markup — it is text content, escaped by goldmark.
	p.AllowElements("pre", "code")
	p.AllowAttrs("class").Matching(langClassRe).OnElements("code", "pre")

	// --- GFM tables -------------------------------------------------------------
	// AllowTables permits table/thead/tbody/tr/th/td; the align attribute carries
	// GFM column alignment (goldmark emits align="left|center|right").
	p.AllowTables()
	p.AllowAttrs("align").Matching(alignRe).OnElements("td", "th", "table")

	// --- GFM task lists ---------------------------------------------------------
	// goldmark renders "- [x]" as a disabled checkbox <input>. Allow only the
	// exact attributes it emits; type is pinned to "checkbox" so no other input
	// kind (e.g. an image/submit/button input) can slip through.
	p.AllowAttrs("type").Matching(regexp.MustCompile(`^checkbox$`)).OnElements("input")
	p.AllowAttrs("checked", "disabled").OnElements("input")
	p.AllowElements("input")

	// --- Footnotes (goldmark Footnote extension) --------------------------------
	// Emits fragment-linked <sup>/<a> refs and a <div class="footnotes"> block
	// with id-bearing <li>. id/role are matched against conservative patterns to
	// permit the footnote anchors ("fn:1", "fnref:1", "doc-noteref") while keeping
	// the attribute closed to arbitrary values (limits DOM-clobbering surface).
	p.AllowAttrs("id").Matching(idRe).OnElements("sup", "li", "div", "a")
	p.AllowAttrs("role").Matching(roleRe).OnElements("a", "div", "ol", "li", "sup")

	// --- Images -----------------------------------------------------------------
	// src runs through the same scheme allowlist as links (no data:/javascript:).
	p.AllowImages()

	// --- KaTeX class allowance (forward-provisioning, 01-architecture.md §4) -----
	// Math renders CLIENT-side (KaTeX, trust:false) over this already-sanitized
	// HTML, so stored content normally carries only literal "$...$" text, not
	// KaTeX markup. Per this ticket's allowlist requirement we still permit the
	// class names KaTeX emits on span/div: classes cannot execute, and pre-allowing
	// them means author-inlined or future server-side math markup is not stripped.
	p.AllowAttrs("class").Matching(safeClassRe).OnElements("span", "div")

	// --- SVG structural allowlist (forward-provisioning for diagrams) -----------
	// LOO-19 (Kroki) is the real producer and OWNS full SVG sanitization — including
	// the case-sensitive-attribute problem (viewBox/preserveAspectRatio are lowered
	// by the HTML parser) and dangerous constructs (<foreignObject>, <use> external
	// refs, xlink:href). This policy only pre-allows the safe *structural* tag/attr
	// surface so M4 does not have to reshape the pipeline: no script, no event
	// handlers, no href/xlink:href, no style are allowed on any of these, so the
	// no-op M1 pipeline (which emits no SVG) stays safe.
	p.AllowElements(
		"svg", "g", "path", "rect", "circle", "ellipse",
		"line", "polyline", "polygon", "text", "tspan",
		"defs", "marker", "symbol", "title", "desc",
	)
	p.AllowAttrs(
		"fill", "stroke", "stroke-width", "stroke-linecap", "stroke-linejoin",
		"d", "points", "x", "y", "x1", "y1", "x2", "y2",
		"cx", "cy", "r", "rx", "ry", "width", "height", "transform",
		"text-anchor", "font-size", "font-family",
	).OnElements(
		"svg", "g", "path", "rect", "circle", "ellipse",
		"line", "polyline", "polygon", "text", "tspan", "marker",
	)
	p.AllowAttrs("class").Matching(safeClassRe).OnElements(
		"svg", "g", "path", "rect", "circle", "ellipse",
		"line", "polyline", "polygon", "text", "tspan",
	)

	return p
}

// langClassRe matches the fenced-code language class goldmark emits.
var langClassRe = regexp.MustCompile(`^language-[\w+.#-]+$`)

// alignRe matches GFM table column alignment values.
var alignRe = regexp.MustCompile(`^(left|center|right)$`)

// idRe matches conservative HTML ids: a letter-led token of word chars plus the
// ":" / "-" / "." that footnote anchors ("fn:1", "fnref:1") use. Rejects spaces
// and anything that could confuse attribute parsing.
var idRe = regexp.MustCompile(`^[A-Za-z][\w:.-]*$`)

// roleRe matches ARIA roles (letters and hyphens only, e.g. "doc-noteref").
var roleRe = regexp.MustCompile(`^[a-z][a-z-]*$`)

// safeClassRe matches whitespace-separated class tokens of the character set
// KaTeX / diagram classes use (word chars, hyphen, colon). No quotes, no angle
// brackets, so the value cannot break out of the attribute.
var safeClassRe = regexp.MustCompile(`^[\w\- :]+$`)
