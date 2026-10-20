package search

import (
	"html"
	"strings"

	xhtml "golang.org/x/net/html"
)

// skipElements hold no prose worth indexing.
var skipElements = map[string]bool{"script": true, "style": true, "svg": true, "math": true}

// blockElements end a run of text, so adjacent blocks don't glue words together.
var blockElements = map[string]bool{
	"p": true, "div": true, "br": true, "li": true, "tr": true, "td": true, "th": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"pre": true, "blockquote": true, "hr": true, "section": true, "table": true,
}

// TextFromHTML extracts searchable plain text from the published, sanitized
// HTML: tags dropped, block boundaries become spaces, whitespace collapsed.
func TextFromHTML(src string) string {
	z := xhtml.NewTokenizer(strings.NewReader(src))
	var b strings.Builder
	skip := 0
	for {
		switch z.Next() {
		case xhtml.ErrorToken:
			return strings.Join(strings.Fields(b.String()), " ")
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			name, _ := z.TagName()
			if skipElements[string(name)] {
				skip++
			}
			if blockElements[string(name)] {
				b.WriteByte(' ')
			}
		case xhtml.EndTagToken:
			name, _ := z.TagName()
			if skipElements[string(name)] && skip > 0 {
				skip--
			}
			if blockElements[string(name)] {
				b.WriteByte(' ')
			}
		case xhtml.TextToken:
			if skip == 0 {
				b.Write(z.Text())
			}
		}
	}
}

// markHTML turns a Meilisearch formatted string (highlights wrapped in the
// control-character markers) into safe HTML: everything is escaped, then only
// the markers become <mark> elements. Unbalanced markers are dropped.
func markHTML(s string) string {
	escaped := html.EscapeString(s)
	var b strings.Builder
	open := false
	for _, r := range escaped {
		switch string(r) {
		case markPre:
			if !open {
				b.WriteString("<mark>")
				open = true
			}
		case markPost:
			if open {
				b.WriteString("</mark>")
				open = false
			}
		default:
			b.WriteRune(r)
		}
	}
	if open {
		b.WriteString("</mark>")
	}
	return b.String()
}
