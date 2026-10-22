<!--
  MarkdownEditor — CodeMirror 6 editor with Obsidian-like live preview. Editor
  logic lives in $lib/editor/* (unit-tested headlessly); this is thin glue.

  Progressive enhancement: SSR renders a plain <textarea name={name}> that is both
  the form field the save action reads and the no-JS fallback. On mount CodeMirror
  is created and the textarea visually hidden; `value` stays two-way bound so the
  form always submits the current source. Preview is client-side and author-only;
  only raw source_md is ever persisted.
-->
<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import type { EditorView } from '@codemirror/view';

	let {
		value = $bindable(''),
		name,
		label,
		placeholder = ''
	}: { value?: string; name: string; label: string; placeholder?: string } = $props();

	let host = $state<HTMLDivElement>();
	let view: EditorView | undefined;
	let ready = $state(false);

	onMount(async () => {
		const { EditorView, createEditorExtensions } = await import('$lib/editor/setup');
		view = new EditorView({
			doc: value,
			parent: host,
			extensions: createEditorExtensions({
				ariaLabel: label,
				placeholder,
				onChange: (doc) => {
					value = doc;
				}
			})
		});
		ready = true;
	});

	onDestroy(() => view?.destroy());

	// Keep the editor in sync if `value` is replaced from outside (e.g. a form reset).
	// Guarded by an equality check so the onChange→value→effect loop is a no-op.
	$effect(() => {
		if (view && value !== view.state.doc.toString()) {
			view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: value } });
		}
	});
</script>

<div class="editor">
	<label class="visually-hidden" for={name}>{label}</label>
	<div class="host" class:hidden={!ready} bind:this={host}></div>
	<textarea
		id={name}
		{name}
		class="fallback"
		class:hidden={ready}
		bind:value
		{placeholder}
		rows="16"
		spellcheck="false"></textarea>
</div>

<style>
	.editor {
		display: grid;
	}

	.host {
		min-height: 50vh;
	}

	.hidden {
		display: none;
	}

	.fallback {
		width: 100%;
		min-height: 50vh;
		padding: 0;
		border: none;
		background: transparent;
		color: var(--fg);
		font-family: var(--font-mono);
		font-size: var(--text-base);
		line-height: 1.7;
		resize: vertical;
		outline: none;
	}
</style>
