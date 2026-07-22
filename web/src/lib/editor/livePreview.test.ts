import { describe, expect, it } from 'vitest';
import { EditorSelection, EditorState } from '@codemirror/state';
import { markdown, markdownLanguage } from '@codemirror/lang-markdown';
import { computePreviewDecorations, type PreviewDecoration } from './livePreview';

// Build a real Markdown EditorState (GFM) and compute the preview decorations. The
// cursor defaults to the document end; callers keep the markup under test on an
// earlier line so it is NOT the active line (raw markup would otherwise be revealed).
function decos(doc: string, cursor = doc.length): PreviewDecoration[] {
	const state = EditorState.create({
		doc,
		selection: EditorSelection.single(cursor),
		extensions: [markdown({ base: markdownLanguage })]
	});
	return computePreviewDecorations(state);
}

// A trailing "\n\nx" keeps the cursor (at end) off the first line under test.
const pad = (s: string) => `${s}\n\nx`;

function marks(ds: PreviewDecoration[], cls: string) {
	return ds.filter((d) => d.kind === 'mark' && d.class === cls);
}
function kinds(ds: PreviewDecoration[], kind: PreviewDecoration['kind']) {
	return ds.filter((d) => d.kind === kind);
}
function sliceMark(doc: string, d: PreviewDecoration) {
	return 'from' in d && 'to' in d ? doc.slice(d.from, d.to) : '';
}

describe('computePreviewDecorations — inline formatting (AC-3.1)', () => {
	it('styles a heading and hides its "# " marker', () => {
		const doc = pad('# Title');
		const ds = decos(doc);
		expect(ds.some((d) => d.kind === 'line' && d.class === 'cm-preview-h1')).toBe(true);
		const hide = kinds(ds, 'hide');
		expect(hide.some((d) => sliceMark(doc, d) === '# ')).toBe(true);
	});

	it('picks the right heading level', () => {
		expect(
			decos(pad('### Sub')).some((d) => d.kind === 'line' && d.class === 'cm-preview-h3')
		).toBe(true);
	});

	it('marks bold and hides the ** delimiters', () => {
		const doc = pad('a **bold** b');
		const ds = decos(doc);
		const m = marks(ds, 'cm-preview-strong');
		expect(m).toHaveLength(1);
		expect(sliceMark(doc, m[0])).toBe('bold');
		expect(kinds(ds, 'hide').filter((d) => sliceMark(doc, d) === '**')).toHaveLength(2);
	});

	it('marks italic, strikethrough and inline code', () => {
		expect(sliceMark(pad('*it*'), marks(decos(pad('*it*')), 'cm-preview-em')[0])).toBe('it');
		expect(marks(decos(pad('~~no~~')), 'cm-preview-strike')).toHaveLength(1);
		expect(marks(decos(pad('`code`')), 'cm-preview-code')).toHaveLength(1);
	});

	it('shows only the link text and hides the brackets/url', () => {
		const doc = pad('[label](https://x)');
		const ds = decos(doc);
		const m = marks(ds, 'cm-preview-link');
		expect(m).toHaveLength(1);
		expect(sliceMark(doc, m[0])).toBe('label');
		const hidden = kinds(ds, 'hide').map((d) => sliceMark(doc, d));
		expect(hidden).toContain('[');
		expect(hidden).toContain('](https://x)');
	});
});

describe('computePreviewDecorations — lists, tasks, quotes (full set)', () => {
	it('renders a bullet marker as a bullet widget', () => {
		const doc = pad('- item');
		const bullets = kinds(decos(doc), 'bullet');
		expect(bullets).toHaveLength(1);
		expect(sliceMark(doc, bullets[0])).toBe('-');
	});

	it('leaves ordered-list markers untouched', () => {
		const ds = decos(pad('1. item'));
		expect(kinds(ds, 'bullet')).toHaveLength(0);
		expect(kinds(ds, 'checkbox')).toHaveLength(0);
	});

	it('renders task items as checkboxes with the right checked state', () => {
		const doc = '- [ ] todo\n- [x] done\n\nx';
		const boxes = kinds(decos(doc), 'checkbox') as Extract<
			PreviewDecoration,
			{ kind: 'checkbox' }
		>[];
		expect(boxes).toHaveLength(2);
		expect(boxes[0].checked).toBe(false);
		expect(sliceMark(doc, boxes[0])).toBe('- [ ] ');
		expect(boxes[1].checked).toBe(true);
		expect(sliceMark(doc, boxes[1])).toBe('- [x] ');
	});

	it('styles a blockquote line and hides its "> " marker', () => {
		const doc = pad('> quoted');
		const ds = decos(doc);
		expect(ds.some((d) => d.kind === 'line' && d.class === 'cm-preview-quote')).toBe(true);
		expect(kinds(ds, 'hide').some((d) => sliceMark(doc, d) === '> ')).toBe(true);
	});
});

describe('computePreviewDecorations — active-line reveal', () => {
	it('reveals raw markup (no hide/widget) on the line holding the cursor', () => {
		const doc = '**bold**\n\nx';
		const onLine = decos(doc, 3); // cursor inside "**bold**"
		expect(kinds(onLine, 'hide')).toHaveLength(0);
		// styling still applies while editing
		expect(marks(onLine, 'cm-preview-strong')).toHaveLength(1);

		const offLine = decos(doc, doc.length); // cursor on the trailing line
		expect(kinds(offLine, 'hide').length).toBeGreaterThan(0);
	});

	it('reveals a task line so the raw "- [ ]" is editable', () => {
		const doc = '- [ ] todo\n\nx';
		expect(kinds(decos(doc, 4), 'checkbox')).toHaveLength(0); // cursor on the task line
		expect(kinds(decos(doc, doc.length), 'checkbox')).toHaveLength(1);
	});
});
