package search

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTextFromHTML(t *testing.T) {
	src := `<h1>Deploy</h1><p>Run <code>make&nbsp;up</code> &amp; wait.</p><ul><li>one</li><li>two</li></ul>` +
		`<script>alert(1)</script><svg><text>diagram</text></svg><p>end</p>`
	require.Equal(t, "Deploy Run make up & wait. one two end", TextFromHTML(src))
}

func TestReadableTextFromHTML(t *testing.T) {
	src := `<h1>Deploy</h1><p>Run <code>make&nbsp;up</code> &amp; wait.</p><ul><li>one</li><li>two <em>three</em></li></ul>` +
		"<pre><code>line 1\n  indented\n</code></pre>" +
		`<script>alert(1)</script><svg><text>diagram</text></svg><table><tr><td>a</td><td>b</td></tr></table><p>end<br>next</p>`
	want := "Deploy\n\nRun make up & wait.\n\n- one\n- two three\n\nline 1\n  indented\n\na b\n\nend\nnext"
	require.Equal(t, want, ReadableTextFromHTML(src))
	require.Equal(t, "", ReadableTextFromHTML(""))
	require.Equal(t, "&lt;b&gt; is text", ReadableTextFromHTML(`<p>&amp;lt;b&amp;gt; is text</p>`),
		"entities decode once: the output is plain text, not HTML")
}

func TestMarkHTML_EscapesEverythingButMarks(t *testing.T) {
	in := `<img src=x onerror=alert(1)> found ` + markPre + `kube` + markPost + `rnetes <mark>`
	out := markHTML(in)
	require.Equal(t, `&lt;img src=x onerror=alert(1)&gt; found <mark>kube</mark>rnetes &lt;mark&gt;`, out)
}

func TestMarkHTML_ClosesUnbalanced(t *testing.T) {
	require.Equal(t, "<mark>open</mark>", markHTML(markPre+"open"))
	require.Equal(t, "stray", markHTML("stray"+markPost))
}

func TestPickSnippet(t *testing.T) {
	var h rawHit
	h.Formatted.BodyText = "plain body"
	h.Formatted.Description = "the description"
	require.Equal(t, "the description", pickSnippet(h, true), "no body match → description")

	h.Formatted.BodyText = "has " + markPre + "match" + markPost
	require.Equal(t, h.Formatted.BodyText, pickSnippet(h, true))
	require.Equal(t, "the description", pickSnippet(h, false), "empty query → description")
}
