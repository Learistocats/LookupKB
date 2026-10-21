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

// paragraphElements are separated by a blank line in ReadableTextFromHTML; the
// other blockElements (li, tr, br) by a single line break.
var paragraphElements = map[string]bool{
	"p": true, "div": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"pre": true, "blockquote": true, "hr": true, "section": true, "table": true, "ul": true, "ol": true,
}

// ReadableTextFromHTML is TextFromHTML for a human or agent reader (the public
// API's ?format=text, MCP get_document; LOO-38): the same tokenizer walk over the
// published, sanitized HTML with the same skipped elements, but block boundaries
// become line breaks (a blank line between paragraphs), list items get a "- "
// marker and <pre> content keeps its whitespace. The output is plain text.
func ReadableTextFromHTML(src string) string {
	z := xhtml.NewTokenizer(strings.NewReader(src))
	var w readableWriter
	skip, pre := 0, 0
	for {
		switch z.Next() {
		case xhtml.ErrorToken:
			return w.String()
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			name, _ := z.TagName()
			tag := string(name)
			switch {
			case skipElements[tag]:
				skip++
			case tag == "li":
				w.lineBreak(1)
				w.text("- ")
			case tag == "td" || tag == "th":
				w.owe()
			case blockElements[tag]:
				w.lineBreak(breakFor(tag))
			}
			if tag == "pre" {
				pre++
			}
		case xhtml.EndTagToken:
			name, _ := z.TagName()
			tag := string(name)
			switch {
			case skipElements[tag]:
				if skip > 0 {
					skip--
				}
			case tag == "td" || tag == "th":
				w.owe()
			case blockElements[tag]:
				w.lineBreak(breakFor(tag))
			}
			if tag == "pre" && pre > 0 {
				pre--
			}
		case xhtml.TextToken:
			if skip > 0 {
				continue
			}
			if pre > 0 {
				w.verbatim(string(z.Text()))
			} else {
				w.text(string(z.Text()))
			}
		}
	}
}

func breakFor(tag string) int {
	if paragraphElements[tag] {
		return 2
	}
	return 1
}

// readableWriter accumulates ReadableTextFromHTML output: whitespace runs collapse
// to one space, line breaks never stack beyond one blank line, and nothing leads
// or trails.
type readableWriter struct {
	b        strings.Builder
	newlines int  // trailing newline count
	owed     bool // a space is owed before the next word
}

func (w *readableWriter) lineBreak(n int) {
	if w.b.Len() == 0 {
		return
	}
	for w.newlines < n {
		w.b.WriteByte('\n')
		w.newlines++
	}
	w.owed = false
}

// owe records a word boundary (only mid-line: a line break already separates).
func (w *readableWriter) owe() {
	if w.b.Len() > 0 && w.newlines == 0 {
		w.owed = true
	}
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

func (w *readableWriter) text(s string) {
	if s == "" {
		return
	}
	if isSpace(s[0]) {
		w.owe()
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return
	}
	if w.owed {
		w.b.WriteByte(' ')
	}
	w.b.WriteString(strings.Join(words, " "))
	w.newlines = 0
	w.owed = isSpace(s[len(s)-1])
}

func (w *readableWriter) verbatim(s string) {
	if s == "" {
		return
	}
	if w.owed {
		w.b.WriteByte(' ')
		w.owed = false
	}
	w.b.WriteString(s)
	w.newlines = len(s) - len(strings.TrimRight(s, "\n"))
}

func (w *readableWriter) String() string {
	return strings.TrimSpace(w.b.String())
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
