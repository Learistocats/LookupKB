// CodeMirror theme + Markdown syntax highlighting for the draft editor (LOO-12).
// Matches the app's hardcoded dark palette (see TagInput.svelte / drafts page) —
// there is no design-token system to reuse, so the values are repeated here.
import { EditorView } from '@codemirror/view';
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language';
import { tags as t } from '@lezer/highlight';

// Shared dark palette (kept in sync with the rest of web/ by hand).
const bg = '#0b0c0e';
const text = '#e8eaed';
const muted = '#9aa0a6';
const accent = '#93c5fd';
const border = '#2a2e35';
const raised = '#131519';
const mono = "ui-monospace, 'SF Mono', Menlo, monospace";

// editorTheme styles the editor surface and the live-preview decoration classes
// emitted by livePreview.ts (cm-preview-*). It is a dark theme (dark: true) so
// CodeMirror's built-in dark defaults apply where we don't override.
export const editorTheme = EditorView.theme(
	{
		'&': {
			color: text,
			backgroundColor: raised,
			border: `1px solid ${border}`,
			borderRadius: '0.5rem',
			fontSize: '0.95rem'
		},
		'&.cm-focused': {
			outline: 'none',
			borderColor: accent
		},
		'.cm-scroller': {
			fontFamily: mono,
			lineHeight: '1.6',
			padding: '0.25rem 0'
		},
		'.cm-content': {
			caretColor: accent,
			padding: '0.5rem 0.75rem'
		},
		'.cm-cursor, .cm-dropCursor': { borderLeftColor: accent },
		'&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection': {
			backgroundColor: '#23324a'
		},
		'.cm-gutters': { display: 'none' },
		'.cm-activeLine': { backgroundColor: 'transparent' },
		// --- live-preview decoration classes (see livePreview.ts) ---
		'.cm-preview-h1': { fontSize: '1.6em', fontWeight: '700', lineHeight: '1.3' },
		'.cm-preview-h2': { fontSize: '1.4em', fontWeight: '700', lineHeight: '1.3' },
		'.cm-preview-h3': { fontSize: '1.2em', fontWeight: '700' },
		'.cm-preview-h4': { fontSize: '1.05em', fontWeight: '700' },
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
	},
	{ dark: true }
);

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
	{ tag: [t.monospace], color: '#a5d6ff' },
	{ tag: [t.processingInstruction, t.contentSeparator], color: muted },
	// Nested code-language tokens.
	{ tag: t.keyword, color: '#ff7b72' },
	{ tag: [t.name, t.deleted, t.character, t.macroName], color: '#e8eaed' },
	{ tag: [t.function(t.variableName), t.labelName], color: '#d2a8ff' },
	{ tag: [t.color, t.constant(t.name), t.standard(t.name)], color: '#79c0ff' },
	{ tag: [t.definition(t.name), t.separator], color: text },
	{ tag: [t.typeName, t.className, t.number, t.changed, t.annotation, t.self], color: '#79c0ff' },
	{ tag: [t.operator, t.operatorKeyword], color: '#ff7b72' },
	{ tag: [t.string, t.inserted], color: '#a5d6ff' },
	{ tag: [t.meta, t.comment], color: muted, fontStyle: 'italic' },
	{ tag: t.invalid, color: '#f87171' }
]);

export const markdownSyntaxHighlighting = syntaxHighlighting(markdownHighlightStyle);
