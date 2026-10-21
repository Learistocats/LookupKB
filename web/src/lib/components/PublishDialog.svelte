<!--
  PublishDialog — the F5 publish confirm step (AC-5.1/5.2). The repo's first modal.
  It is the SECOND click of a genuinely two-step flow: the editor's "Publish" button
  opens this dialog showing the metadata about to be published (title, description,
  tags, access scope); the author reviews/edits it and must click "Confirm publish"
  — a distinct action — before the document changes state.

  Presentational: it binds the metadata and renders any server per-field errors
  inline; the page owns the publish request (see $lib/publish). On a 422 the page
  feeds `fieldErrors` back in and keeps the dialog open, so the document stays a
  draft and the editor content is preserved. The modal chrome (native <dialog>,
  header, Cancel/Confirm) is the shared ConfirmDialog; this adds the metadata form.
-->
<script lang="ts">
	import ConfirmDialog from './ConfirmDialog.svelte';
	import TagInput from './TagInput.svelte';
	import type { FieldError } from '$lib/publish';

	let {
		open = $bindable(false),
		title = $bindable(''),
		description = $bindable(''),
		tags = $bindable<string[]>([]),
		fieldErrors = [],
		busy = false,
		confirmLabel = 'Publish',
		onConfirm,
		onCancel
	}: {
		open?: boolean;
		title?: string;
		description?: string;
		tags?: string[];
		fieldErrors?: FieldError[];
		busy?: boolean;
		confirmLabel?: string;
		onConfirm: () => void;
		onCancel: () => void;
	} = $props();

	// Per-field lookup for inline errors beside the offending input.
	function errorsFor(field: string): string[] {
		return fieldErrors.filter((e) => e.field === field).map((e) => e.message);
	}

	// Errors not tied to an editable field (e.g. the document being too large).
	let otherErrors = $derived(
		fieldErrors.filter((e) => !['title', 'description', 'tags'].includes(e.field))
	);
</script>

<ConfirmDialog
	bind:open
	ariaLabel="Publish document"
	title={confirmLabel === 'Update' ? 'Update published entry' : 'Publish entry'}
	message={confirmLabel === 'Update'
		? 'Readers will see your latest changes once you confirm.'
		: 'Check the details readers and search will see, then confirm.'}
	confirmLabel={`Confirm ${confirmLabel.toLowerCase()}`}
	{busy}
	{onConfirm}
	{onCancel}
>
	{#each otherErrors as err (err.message)}
		<p class="error-text">{err.message}</p>
	{/each}

	<div class="field">
		<label class="field-label" for="publish-title">Title</label>
		<input
			id="publish-title"
			class="input"
			bind:value={title}
			placeholder="A clear, descriptive title"
		/>
		{#each errorsFor('title') as err (err)}
			<p class="error-text">{err}</p>
		{/each}
	</div>

	<div class="field">
		<label class="field-label" for="publish-description">Description</label>
		<textarea
			id="publish-description"
			class="input"
			bind:value={description}
			rows="3"
			placeholder="One or two sentences on what this entry covers"></textarea>
		{#each errorsFor('description') as err (err)}
			<p class="error-text">{err}</p>
		{/each}
	</div>

	<div class="field">
		<TagInput bind:tags />
		{#each errorsFor('tags') as err (err)}
			<p class="error-text">{err}</p>
		{/each}
	</div>

	<p class="access">Visible to everyone signed in to this knowledge base.</p>
</ConfirmDialog>

<style>
	textarea {
		resize: vertical;
	}

	.access {
		margin: 0;
		color: var(--fg-subtle);
		font-size: var(--text-sm);
	}
</style>
