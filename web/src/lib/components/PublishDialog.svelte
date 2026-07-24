<!--
  PublishDialog — the F5 publish confirm step (AC-5.1/5.2). The repo's first modal.
  It is the SECOND click of a genuinely two-step flow: the editor's "Publish" button
  opens this dialog showing the metadata about to be published (title, description,
  tags, access scope); the author reviews/edits it and must click "Confirm publish"
  — a distinct action — before the document changes state.

  Presentational: it binds the metadata and renders any server per-field errors
  inline; the page owns the publish request (see $lib/publish). On a 422 the page
  feeds `fieldErrors` back in and keeps the dialog open, so the document stays a
  draft and the editor content is preserved.
-->
<script lang="ts">
	import TagInput from './TagInput.svelte';
	import type { FieldError } from '$lib/publish';

	let {
		open = $bindable(false),
		title = $bindable(''),
		description = $bindable(''),
		tags = $bindable<string[]>([]),
		fieldErrors = [],
		busy = false,
		onConfirm,
		onCancel
	}: {
		open?: boolean;
		title?: string;
		description?: string;
		tags?: string[];
		fieldErrors?: FieldError[];
		busy?: boolean;
		onConfirm: () => void;
		onCancel: () => void;
	} = $props();

	let dialogEl = $state<HTMLDialogElement>();

	// Drive the native <dialog> from the `open` prop so the parent controls it.
	$effect(() => {
		if (!dialogEl) return;
		if (open && !dialogEl.open) dialogEl.showModal();
		else if (!open && dialogEl.open) dialogEl.close();
	});

	// Per-field lookup for inline errors beside the offending input.
	function errorsFor(field: string): string[] {
		return fieldErrors.filter((e) => e.field === field).map((e) => e.message);
	}

	// Errors not tied to an editable field (e.g. the document being too large).
	let otherErrors = $derived(
		fieldErrors.filter((e) => !['title', 'description', 'tags'].includes(e.field))
	);
</script>

<dialog bind:this={dialogEl} onclose={onCancel} aria-label="Publish document">
	<h2>Publish document</h2>
	<p class="hint">Review the metadata that will be published, then confirm.</p>

	{#each otherErrors as err (err.message)}
		<p class="badge error">{err.message}</p>
	{/each}

	<label>
		Title
		<input bind:value={title} placeholder="A clear, descriptive title" />
	</label>
	{#each errorsFor('title') as err (err)}
		<span class="badge error">{err}</span>
	{/each}

	<label>
		Description
		<textarea bind:value={description} rows="3" placeholder="A short summary of this document"
		></textarea>
	</label>
	{#each errorsFor('description') as err (err)}
		<span class="badge error">{err}</span>
	{/each}

	<TagInput bind:tags />
	{#each errorsFor('tags') as err (err)}
		<span class="badge error">{err}</span>
	{/each}

	<p class="access">Access: <strong>Public</strong></p>

	<div class="actions">
		<button type="button" class="secondary" onclick={onCancel} disabled={busy}>Cancel</button>
		<button type="button" class="primary" onclick={onConfirm} disabled={busy}>
			{busy ? 'Publishing…' : 'Confirm publish'}
		</button>
	</div>
</dialog>

<style>
	dialog {
		width: min(32rem, 92vw);
		border: 1px solid #2a2e35;
		border-radius: 0.75rem;
		background: #131519;
		color: #e8eaed;
		padding: 1.5rem;
	}

	dialog::backdrop {
		background: rgba(0, 0, 0, 0.6);
	}

	h2 {
		margin: 0;
		font-size: 1.25rem;
	}

	.hint {
		color: #9aa0a6;
		font-size: 0.9rem;
		margin: 0.25rem 0 1rem;
	}

	label {
		display: grid;
		gap: 0.4rem;
		margin-top: 0.75rem;
		color: #9aa0a6;
		font-size: 0.85rem;
		text-transform: uppercase;
		letter-spacing: 0.06em;
	}

	input,
	textarea {
		width: 100%;
		box-sizing: border-box;
		padding: 0.6rem 0.75rem;
		border: 1px solid #2a2e35;
		border-radius: 0.5rem;
		background: #0f1114;
		color: #e8eaed;
		font: inherit;
		resize: vertical;
	}

	.access {
		color: #9aa0a6;
		font-size: 0.9rem;
		margin: 1rem 0 0;
	}

	.actions {
		display: flex;
		justify-content: flex-end;
		gap: 0.75rem;
		margin-top: 1.5rem;
	}

	button {
		padding: 0.5rem 1.25rem;
		border: 1px solid #2a2e35;
		border-radius: 0.5rem;
		background: #1a1d22;
		color: #e8eaed;
		cursor: pointer;
		font: inherit;
	}

	button:hover:not(:disabled) {
		background: #23272e;
	}

	button:disabled {
		opacity: 0.6;
		cursor: default;
	}

	.primary {
		background: #1d3a5f;
		border-color: #285285;
	}

	.primary:hover:not(:disabled) {
		background: #24466f;
	}

	.badge {
		display: inline-block;
		padding: 0.25rem 0.6rem;
		border-radius: 999px;
		font-weight: 600;
		font-size: 0.85rem;
		margin-top: 0.35rem;
	}

	.badge.error {
		background: #2e1113;
		color: #f87171;
	}
</style>
