<!--
  MarkdownEditor — CodeMirror 6 editor with Obsidian-like live preview (LOO-12,
  AC-3.1/3.2/3.6). All editor logic lives in $lib/editor/* so it is unit-tested
  headlessly; this component is thin glue (the TagInput.svelte convention).

  Progressive enhancement: SSR renders a plain <textarea name={name}> that is both
  the form field the save action reads AND the no-JS fallback. On mount (client only)
  CodeMirror is created and the textarea is visually hidden; `value` stays two-way
  bound so the form always submits the current source. Preview is client-side and
  author-only — only raw source_md is ever persisted (never preview HTML).
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
	<label for={name}>{label}</label>
	<div class="host" class:hidden={!ready} bind:this={host}></div>
	<textarea
		id={name}
		{name}
		class="fallback"
		class:hidden={ready}
		bind:value
		{placeholder}
		rows="16"
		spellcheck="false"
	></textarea>
</div>

<style>
	.editor {
		display: grid;
		gap: 0.4rem;
	}

	label {
		color: #9aa0a6;
		font-size: 0.85rem;
		text-transform: uppercase;
		letter-spacing: 0.06em;
	}

	.host {
		min-height: 20rem;
	}

	.hidden {
		display: none;
	}

	.fallback {
		width: 100%;
		box-sizing: border-box;
		padding: 0.6rem 0.75rem;
		border: 1px solid #2a2e35;
		border-radius: 0.5rem;
		background: #131519;
		color: #e8eaed;
		font-family: ui-monospace, 'SF Mono', Menlo, monospace;
		font-size: 0.9rem;
		line-height: 1.5;
		resize: vertical;
	}
</style>
