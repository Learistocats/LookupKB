// Markdown formatting commands + keymap for the editor (LOO-12, AC-3.6).
//
// The wrap/link logic is pure (operates on EditorState, returns a TransactionSpec)
// so it is unit-tested headlessly; the exported Commands are thin view.dispatch
// wrappers bound to the conventional shortcuts.
import { EditorSelection, type EditorState, type TransactionSpec } from '@codemirror/state';
import type { Command, KeyBinding } from '@codemirror/view';

// wrapSelection toggles `before…after` around each selection range: it wraps the
// selection, or unwraps when the delimiters are already immediately outside it
// (so Mod-B on already-bold text removes the bold). Empty selections get an empty
// pair with the cursor placed between the delimiters.
export function wrapSelection(state: EditorState, before: string, after = before): TransactionSpec {
	return state.changeByRange((range) => {
		const { from, to } = range;
		const hasBefore =
			from >= before.length && state.sliceDoc(from - before.length, from) === before;
		const hasAfter = state.sliceDoc(to, to + after.length) === after;

		if (hasBefore && hasAfter) {
			return {
				changes: [
					{ from: from - before.length, to: from },
					{ from: to, to: to + after.length }
				],
				range: EditorSelection.range(from - before.length, to - before.length)
			};
		}

		return {
			changes: [
				{ from, insert: before },
				{ from: to, insert: after }
			],
			range: EditorSelection.range(from + before.length, to + before.length)
		};
	});
}

// insertLink replaces the selection with `[selection](url)` and selects the `url`
// placeholder so the author can type the target immediately.
export function insertLink(state: EditorState): TransactionSpec {
	const placeholder = 'url';
	return state.changeByRange((range) => {
		const text = state.sliceDoc(range.from, range.to);
		const insert = `[${text}](${placeholder})`;
		const urlFrom = range.from + 1 + text.length + 2; // past "[text]("
		return {
			changes: { from: range.from, to: range.to, insert },
			range: EditorSelection.range(urlFrom, urlFrom + placeholder.length)
		};
	});
}

export const toggleBold: Command = (view) => {
	view.dispatch(wrapSelection(view.state, '**'));
	return true;
};

export const toggleItalic: Command = (view) => {
	view.dispatch(wrapSelection(view.state, '*'));
	return true;
};

export const toggleInlineCode: Command = (view) => {
	view.dispatch(wrapSelection(view.state, '`'));
	return true;
};

export const linkSelection: Command = (view) => {
	view.dispatch(insertLink(view.state));
	return true;
};

// Conventional shortcuts (Mod = Cmd on macOS, Ctrl elsewhere). preventDefault so the
// browser's own Ctrl-B/I don't also fire. Links use Mod-Shift-k because Mod-k is the
// app-wide "open search" shortcut (routes/(app)/+layout.svelte).
export const markdownKeymap: readonly KeyBinding[] = [
	{ key: 'Mod-b', run: toggleBold, preventDefault: true },
	{ key: 'Mod-i', run: toggleItalic, preventDefault: true },
	{ key: 'Mod-e', run: toggleInlineCode, preventDefault: true },
	{ key: 'Mod-Shift-k', run: linkSelection, preventDefault: true }
];
