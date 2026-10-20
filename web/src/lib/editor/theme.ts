// CodeMirror theme + Markdown syntax highlighting for the entry editor (LOO-12).
import { EditorView } from '@codemirror/view';
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language';
import { tags as t } from '@lezer/highlight';

// Every color is a design token (web/src/app.css), so the editor follows the
// light/dark theme without re-creating the view.
const bg = 'var(--surface-2)';
const text = 'var(--fg)';
const muted = 'var(--fg-muted)';
const accent = 'var(--accent)';
const border = 'var(--border-strong)';
const mono = 'var(--font-mono)';

// editorTheme styles the editor surface and the live-preview decoration classes
// emitted by livePreview.ts (cm-preview-*). It sits borderless on the page, like
// the title above it.
export const editorTheme = EditorView.theme({
	'&': {
		color: text,
		backgroundColor: 'transparent',
		fontSize: 'var(--text-base)'
	},
	'&.cm-focused': {
		outline: 'none'
	},
	'.cm-scroller': {
		fontFamily: mono,
		lineHeight: '1.7',
		padding: '0'
	},
	'.cm-content': {
		caretColor: accent,
		padding: '0',
		minHeight: '50vh'
	},
	'.cm-placeholder': { color: 'var(--fg-subtle)' },
	// Align the text with the title input above (CodeMirror pads lines by default).
	'.cm-line': { padding: '0' },
	'.cm-cursor, .cm-dropCursor': { borderLeftColor: accent },
	'&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection': {
		backgroundColor: 'var(--accent-soft)'
	},
	'.cm-gutters': { display: 'none' },
	'.cm-activeLine': { backgroundColor: 'transparent' },
	// --- live-preview decoration classes (see livePreview.ts) ---
	'.cm-preview-h1': {
		fontFamily: 'var(--font-ui)',
		fontSize: '1.6em',
		fontWeight: '700',
		lineHeight: '1.3'
	},
	'.cm-preview-h2': {
		fontFamily: 'var(--font-ui)',
		fontSize: '1.35em',
		fontWeight: '700',
		lineHeight: '1.3'
	},
	'.cm-preview-h3': { fontFamily: 'var(--font-ui)', fontSize: '1.15em', fontWeight: '700' },
	'.cm-preview-h4': { fontFamily: 'var(--font-ui)', fontSize: '1.05em', fontWeight: '700' },
	'.cm-preview-h5': { fontWeight: '700', color: muted },
	'.cm-preview-h6': { fontWeight: '700', color: muted },
	'.cm-preview-strong': { fontWeight: '700' },
	'.cm-preview-em': { fontStyle: 'italic' },
	'.cm-preview-strike': { textDecoration: 'line-through', color: muted },
	'.cm-preview-code': {
		fontFamily: mono,
		backgroundColor: bg,
		borderRadius: '0.25rem',
		padding: '0.05em 0.3em'
	},
	'.cm-preview-link': { color: accent, textDecoration: 'underline', cursor: 'pointer' },
	'.cm-preview-quote': {
		borderLeft: `3px solid ${border}`,
		paddingLeft: '0.75rem',
		color: muted
	},
	'.cm-preview-codeblock': {
		backgroundColor: bg,
		borderLeft: `3px solid ${border}`,
		paddingLeft: '0.75rem'
	},
	'.cm-preview-bullet': { color: accent, paddingRight: '0.4em' },
	'.cm-preview-checkbox': {
		marginRight: '0.4em',
		verticalAlign: 'middle',
		accentColor: accent
	}
});

// markdownHighlightStyle colours both Markdown structure and the tokens of nested
// fenced-code languages (AC-3.2). Nested grammars are lazy-loaded via
// @codemirror/language-data (see setup.ts).
const markdownHighlightStyle = HighlightStyle.define([
	{ tag: [t.heading1, t.heading2, t.heading3, t.heading4, t.heading5, t.heading6], color: text },
	{ tag: t.strong, fontWeight: '700' },
	{ tag: t.emphasis, fontStyle: 'italic' },
	{ tag: t.strikethrough, textDecoration: 'line-through' },
	{ tag: t.link, color: accent },
	{ tag: t.url, color: muted },
	{ tag: [t.monospace], color: 'var(--accent)' },
	{ tag: [t.processingInstruction, t.contentSeparator], color: muted },
	// Nested code-language tokens, kept to a few theme-aware hues.
	{ tag: [t.keyword, t.operatorKeyword, t.operator], color: 'var(--accent)' },
	{
		tag: [t.name, t.deleted, t.character, t.macroName, t.definition(t.name), t.separator],
		color: text
	},
	{ tag: [t.function(t.variableName), t.labelName], color: 'var(--accent)' },
	{
		tag: [
			t.color,
			t.constant(t.name),
			t.standard(t.name),
			t.typeName,
			t.className,
			t.number,
			t.changed,
			t.annotation,
			t.self
		],
		color: 'var(--draft)'
	},
	{ tag: [t.string, t.inserted], color: 'var(--ok)' },
	{ tag: [t.meta, t.comment], color: muted, fontStyle: 'italic' },
	{ tag: t.invalid, color: 'var(--danger)' }
]);

export const markdownSyntaxHighlighting = syntaxHighlighting(markdownHighlightStyle);
