package render

import (
	"regexp"

	"github.com/microcosm-cc/bluemonday"
)

// newPolicy builds the strict allowlist applied to every publish-time render.
// It starts from an empty NewPolicy() (not UGCPolicy) so the whole allowed
// surface is readable here: no script, on* handlers or style; href/src limited
// to http/https/mailto plus relative/fragment refs; class/id/role only through
// conservative regexes.
func newPolicy() *bluemonday.Policy {
	p := bluemonday.NewPolicy()

	// Links. Relative/fragment URLs (footnote "#fn:1") carry no scheme and are safe.
	p.AllowStandardURLs()
	p.AllowURLSchemes("http", "https", "mailto")
	p.AllowRelativeURLs(true)
	p.RequireNoFollowOnLinks(true)
	p.AllowAttrs("href", "rel").OnElements("a")
	p.AllowAttrs("target").Matching(regexp.MustCompile(`^_blank$`)).OnElements("a")

	p.AllowElements(
		"h1", "h2", "h3", "h4", "h5", "h6",
		"p", "br", "hr", "blockquote",
		"ul", "ol", "li",
		"strong", "em", "del", "s", "b", "i", "u",
		"sup", "sub", "span", "div", "a",
	)

	// Fenced code: keep goldmark's language-* class.
	p.AllowElements("pre", "code")
	p.AllowAttrs("class").Matching(langClassRe).OnElements("code", "pre")

	// GFM tables; align carries column alignment.
	p.AllowTables()
	p.AllowAttrs("align").Matching(alignRe).OnElements("td", "th", "table")

	// Task lists: a disabled checkbox <input>; type is pinned so no other input kind passes.
	p.AllowAttrs("type").Matching(regexp.MustCompile(`^checkbox$`)).OnElements("input")
	p.AllowAttrs("checked", "disabled").OnElements("input")
	p.AllowElements("input")

	// Footnotes: id/role stay closed to arbitrary values to limit DOM clobbering.
	p.AllowAttrs("id").Matching(idRe).OnElements("sup", "li", "div", "a")
	p.AllowAttrs("role").Matching(roleRe).OnElements("a", "div", "ol", "li", "sup")

	// Images: src uses the same scheme allowlist as links.
	p.AllowImages()

	// Math renders client-side over sanitized HTML; allow the KaTeX class names
	// (classes cannot execute).
	p.AllowAttrs("class").Matching(safeClassRe).OnElements("span", "div")

	return p
}

// langClassRe matches the fenced-code language class goldmark emits.
var langClassRe = regexp.MustCompile(`^language-[\w+.#-]+$`)

// alignRe matches GFM table column alignment values.
var alignRe = regexp.MustCompile(`^(left|center|right)$`)

// idRe matches letter-led ids incl. footnote anchors ("fn:1", "fnref:1").
var idRe = regexp.MustCompile(`^[A-Za-z][\w:.-]*$`)

// roleRe matches ARIA roles (letters and hyphens only, e.g. "doc-noteref").
var roleRe = regexp.MustCompile(`^[a-z][a-z-]*$`)

// safeClassRe matches class tokens (word chars, hyphen, colon, space); no quotes
// or angle brackets, so the value cannot break out of the attribute.
var safeClassRe = regexp.MustCompile(`^[\w\- :]+$`)
