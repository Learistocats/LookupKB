import { describe, expect, it } from 'vitest';
import { EditorSelection, EditorState } from '@codemirror/state';
import { wrapSelection, insertLink } from './commands';

// Build a state with `text` and a selection over [from,to] (a cursor when equal).
function stateWith(text: string, from: number, to = from): EditorState {
	return EditorState.create({ doc: text, selection: EditorSelection.single(from, to) });
}

// Apply a TransactionSpec and return the resulting doc + primary selection.
function apply(state: EditorState, spec: ReturnType<typeof wrapSelection>) {
	const next = state.update(spec).state;
	return { doc: next.doc.toString(), sel: next.selection.main };
}

describe('wrapSelection', () => {
	it('wraps the selection with the delimiters and keeps it selected', () => {
		const state = stateWith('hello world', 6, 11); // "world"
		const next = state.update(wrapSelection(state, '**')).state;
		expect(next.doc.toString()).toBe('hello **world**');
		expect(next.sliceDoc(next.selection.main.from, next.selection.main.to)).toBe('world');
	});

	it('unwraps when the selection is already wrapped (toggle off)', () => {
		const state = stateWith('hello **world**', 8, 13); // inner "world"
		const next = state.update(wrapSelection(state, '**')).state;
		expect(next.doc.toString()).toBe('hello world');
		expect(next.sliceDoc(next.selection.main.from, next.selection.main.to)).toBe('world');
	});

	it('inserts an empty pair with the cursor between for an empty selection', () => {
		const state = stateWith('ab', 1); // cursor between a|b
		const { doc, sel } = apply(state, wrapSelection(state, '*'));
		expect(doc).toBe('a**b');
		expect(sel.empty).toBe(true);
		expect(sel.head).toBe(2); // between the two '*'
	});

	it('supports asymmetric delimiters', () => {
		const state = stateWith('x', 0, 1);
		expect(apply(state, wrapSelection(state, '`', '`')).doc).toBe('`x`');
	});
});

describe('insertLink', () => {
	it('wraps the selection as a link and selects the url placeholder', () => {
		const state = stateWith('click here', 0, 5); // "click"
		const next = state.update(insertLink(state)).state;
		expect(next.doc.toString()).toBe('[click](url) here');
		expect(next.sliceDoc(next.selection.main.from, next.selection.main.to)).toBe('url');
	});

	it('produces [](url) for an empty selection', () => {
		const state = stateWith('', 0);
		const next = state.update(insertLink(state)).state;
		expect(next.doc.toString()).toBe('[](url)');
		expect(next.sliceDoc(next.selection.main.from, next.selection.main.to)).toBe('url');
	});
});
