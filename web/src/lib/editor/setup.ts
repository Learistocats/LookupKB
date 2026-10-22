// Assembles the full CodeMirror 6 extension set for the entry editor.
// Imported dynamically (client-only) by MarkdownEditor.svelte so CodeMirror lands in
// its own chunk and never runs during SSR.
import { EditorView, keymap, placeholder } from '@codemirror/view';
import { history, defaultKeymap, historyKeymap } from '@codemirror/commands';
import { markdown, markdownLanguage } from '@codemirror/lang-markdown';
import { languages } from '@codemirror/language-data';
import type { Extension } from '@codemirror/state';

import { editorTheme, markdownSyntaxHighlighting } from './theme';
import { livePreview } from './livePreview';
import { markdownKeymap } from './commands';

export interface EditorOptions {
	// Called with the full document text whenever it changes (drives bind:value).
	onChange: (doc: string) => void;
	// Accessible name for the editing surface (NFR-4).
	ariaLabel: string;
	// Hint shown while the document is empty.
	placeholder?: string;
}

// createEditorExtensions builds the extension list. GFM (tables, task lists,
// strikethrough, autolinks) comes from markdownLanguage; `codeLanguages: languages`
// lazily loads per-language grammars for fenced-code highlighting.
export function createEditorExtensions(opts: EditorOptions): Extension[] {
	return [
		markdown({ base: markdownLanguage, codeLanguages: languages }),
		history(),
		// markdownKeymap first so Mod-B/I/E and Mod-Shift-K win over any default binding. Tab is
		// deliberately left unbound so it moves focus out of the editor (keyboard
		// tab-order / NFR-4) rather than inserting indentation.
		keymap.of([...markdownKeymap, ...defaultKeymap, ...historyKeymap]),
		EditorView.lineWrapping,
		editorTheme,
		markdownSyntaxHighlighting,
		livePreview(),
		...(opts.placeholder ? [placeholder(opts.placeholder)] : []),
		EditorView.contentAttributes.of({
			'aria-label': opts.ariaLabel,
			role: 'textbox',
			'aria-multiline': 'true'
		}),
		EditorView.updateListener.of((u) => {
			if (u.docChanged) opts.onChange(u.state.doc.toString());
		})
	];
}

// Re-export so the component only needs one dynamic import.
export { EditorView };
