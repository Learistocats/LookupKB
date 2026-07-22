// Obsidian-style Markdown live preview for CodeMirror 6 (LOO-12, AC-3.1).
//
// The decoration *computation* is a pure function over EditorState
// (computePreviewDecorations) that returns plain descriptors, so it is unit-tested
// headlessly in Node with no DOM. The thin ViewPlugin below maps those descriptors
// onto real CodeMirror decorations/widgets in the browser.
//
// Behaviour: formatting marks (bold/italic/strike/code/link/headings) are always
// styled; the raw Markdown *delimiters* (** ` # ~~ [](...)) are hidden — except on
// any line that intersects the selection, where the raw markup is revealed so the
// author can edit it (Obsidian's active-line behaviour).
//
// This module is the single, isolated extension point M4 grows for diagram / math /
// SMILES block widgets — add new descriptor kinds here, not in the core wiring.
import type { EditorState } from '@codemirror/state';
import {
	Decoration,
	type DecorationSet,
	ViewPlugin,
	type ViewUpdate,
	WidgetType
} from '@codemirror/view';
import { ensureSyntaxTree, syntaxTree } from '@codemirror/language';
import type { SyntaxNode } from '@lezer/common';

// A plain, DOM-free description of one decoration. `mark`/`line` style existing text
// and always apply; `hide`/`bullet`/`checkbox` replace the raw markup and are
// suppressed on the active line (the "reveal" kinds).
export type PreviewDecoration =
	| { kind: 'mark'; from: number; to: number; class: string }
	| { kind: 'line'; from: number; class: string }
	| { kind: 'hide'; from: number; to: number }
	| { kind: 'bullet'; from: number; to: number }
	| { kind: 'checkbox'; from: number; to: number; checked: boolean };

const REVEAL_KINDS = new Set(['hide', 'bullet', 'checkbox']);

// Lines (1-based numbers) touched by any selection range — where raw markup shows.
function activeLines(state: EditorState): Set<number> {
	const lines = new Set<number>();
	for (const range of state.selection.ranges) {
		const first = state.doc.lineAt(range.from).number;
		const last = state.doc.lineAt(range.to).number;
		for (let n = first; n <= last; n++) lines.add(n);
	}
	return lines;
}

// Advance past spaces/tabs so a hidden marker also swallows its trailing whitespace
// (e.g. "# " and "> " hide fully, not just the punctuation).
function skipSpaces(state: EditorState, pos: number): number {
	const end = state.doc.length;
	while (pos < end) {
		const ch = state.sliceDoc(pos, pos + 1);
		if (ch !== ' ' && ch !== '\t') break;
		pos++;
	}
	return pos;
}

// computePreviewDecorations walks the Markdown syntax tree and returns the live-
// preview decorations for the whole document, with active-line reveal already
// applied. Pure and headless — the ViewPlugin and the unit tests share it.
export function computePreviewDecorations(state: EditorState): PreviewDecoration[] {
	const tree = ensureSyntaxTree(state, state.doc.length, 100) ?? syntaxTree(state);
	const active = activeLines(state);
	const out: PreviewDecoration[] = [];

	const lineOf = (pos: number) => state.doc.lineAt(pos).number;
	const push = (d: PreviewDecoration) => {
		if (REVEAL_KINDS.has(d.kind) && active.has(lineOf(d.from))) return;
		if ((d.kind === 'mark' || d.kind === 'hide') && d.from >= d.to) return;
		out.push(d);
	};

	const hideWrap = (node: SyntaxNode, markType: string, cls: string) => {
		const marks = node.getChildren(markType);
		if (marks.length < 2) return;
		const open = marks[0];
		const close = marks[marks.length - 1];
		push({ kind: 'mark', from: open.to, to: close.from, class: cls });
		push({ kind: 'hide', from: open.from, to: open.to });
		push({ kind: 'hide', from: close.from, to: close.to });
	};

	tree.iterate({
		enter: (ref) => {
			const name = ref.name;

			if (/^ATXHeading[1-6]$/.test(name)) {
				const level = name.charCodeAt(name.length - 1) - 48;
				push({
					kind: 'line',
					from: state.doc.lineAt(ref.from).from,
					class: `cm-preview-h${level}`
				});
				const marker = ref.node.getChildren('HeaderMark')[0];
				if (marker) push({ kind: 'hide', from: ref.from, to: skipSpaces(state, marker.to) });
				return;
			}

			switch (name) {
				case 'StrongEmphasis':
					hideWrap(ref.node, 'EmphasisMark', 'cm-preview-strong');
					break;
				case 'Emphasis':
					hideWrap(ref.node, 'EmphasisMark', 'cm-preview-em');
					break;
				case 'Strikethrough':
					hideWrap(ref.node, 'StrikethroughMark', 'cm-preview-strike');
					break;
				case 'InlineCode':
					hideWrap(ref.node, 'CodeMark', 'cm-preview-code');
					break;
				case 'Link': {
					const marks = ref.node.getChildren('LinkMark');
					if (marks.length < 2) break;
					const open = marks[0]; // '['
					const closeBracket = marks[1]; // ']'
					push({ kind: 'mark', from: open.to, to: closeBracket.from, class: 'cm-preview-link' });
					push({ kind: 'hide', from: open.from, to: open.to });
					push({ kind: 'hide', from: closeBracket.from, to: ref.to }); // ']( url )'
					break;
				}
				case 'QuoteMark': {
					push({ kind: 'line', from: state.doc.lineAt(ref.from).from, class: 'cm-preview-quote' });
					push({ kind: 'hide', from: ref.from, to: skipSpaces(state, ref.to) });
					break;
				}
				case 'FencedCode': {
					const marks = ref.node.getChildren('CodeMark');
					const info = ref.node.getChildren('CodeInfo')[0];
					// Hide the opening fence + language tag (```javascript) and the
					// closing fence (```) — revealed on the active line like other markup.
					if (marks.length) {
						push({ kind: 'hide', from: ref.from, to: info ? info.to : marks[0].to });
					}
					if (marks.length > 1) {
						const close = marks[marks.length - 1];
						push({ kind: 'hide', from: close.from, to: close.to });
					}
					// Shade every line of the block so the collapsed fence lines read as
					// the block's padding rather than blank gaps.
					const first = state.doc.lineAt(ref.from).number;
					const last = state.doc.lineAt(ref.to).number;
					for (let n = first; n <= last; n++) {
						push({ kind: 'line', from: state.doc.line(n).from, class: 'cm-preview-codeblock' });
					}
					break;
				}
				case 'ListItem': {
					const task = ref.node.getChildren('Task')[0];
					const listMark = ref.node.getChildren('ListMark')[0];
					if (task && listMark) {
						const marker = task.getChildren('TaskMarker')[0];
						if (marker) {
							const checked = /\[[xX]\]/.test(state.sliceDoc(marker.from, marker.to));
							push({
								kind: 'checkbox',
								from: listMark.from,
								to: skipSpaces(state, marker.to),
								checked
							});
						}
						break;
					}
					if (listMark) {
						const glyph = state.sliceDoc(listMark.from, listMark.to);
						if (glyph === '-' || glyph === '*' || glyph === '+') {
							push({ kind: 'bullet', from: listMark.from, to: listMark.to });
						}
					}
					break;
				}
			}
		}
	});

	return out;
}

class BulletWidget extends WidgetType {
	eq() {
		return true;
	}
	toDOM() {
		const span = document.createElement('span');
		span.textContent = '•';
		span.className = 'cm-preview-bullet';
		span.setAttribute('aria-hidden', 'true');
		return span;
	}
}

class CheckboxWidget extends WidgetType {
	constructor(readonly checked: boolean) {
		super();
	}
	eq(other: CheckboxWidget) {
		return other.checked === this.checked;
	}
	toDOM() {
		// Build the node directly (never innerHTML) so a malicious paste can't run
		// script even in the author's own preview. Read-only: clicks/focus are
		// swallowed; the raw "- [ ]" text is revealed on the active line for editing.
		const box = document.createElement('input');
		box.type = 'checkbox';
		box.checked = this.checked;
		box.className = 'cm-preview-checkbox';
		box.setAttribute('aria-hidden', 'true');
		box.tabIndex = -1;
		box.onmousedown = (e) => e.preventDefault();
		box.onclick = (e) => e.preventDefault();
		return box;
	}
	ignoreEvent() {
		return false;
	}
}

function buildDecorations(state: EditorState): DecorationSet {
	const ranges = [];
	for (const d of computePreviewDecorations(state)) {
		switch (d.kind) {
			case 'mark':
				ranges.push(Decoration.mark({ class: d.class }).range(d.from, d.to));
				break;
			case 'line':
				ranges.push(Decoration.line({ class: d.class }).range(d.from));
				break;
			case 'hide':
				ranges.push(Decoration.replace({}).range(d.from, d.to));
				break;
			case 'bullet':
				ranges.push(Decoration.replace({ widget: new BulletWidget() }).range(d.from, d.to));
				break;
			case 'checkbox':
				ranges.push(
					Decoration.replace({ widget: new CheckboxWidget(d.checked) }).range(d.from, d.to)
				);
				break;
		}
	}
	return Decoration.set(ranges, true);
}

// livePreview is the editor extension: it rebuilds the decoration set whenever the
// document or selection changes (selection drives the active-line raw-markup reveal).
export function livePreview() {
	return ViewPlugin.fromClass(
		class {
			decorations: DecorationSet;
			constructor(view: { state: EditorState }) {
				this.decorations = buildDecorations(view.state);
			}
			update(u: ViewUpdate) {
				if (u.docChanged || u.selectionSet || u.viewportChanged) {
					this.decorations = buildDecorations(u.state);
				}
			}
		},
		{ decorations: (v) => v.decorations }
	);
}
