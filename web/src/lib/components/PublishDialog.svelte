<!--
  PublishDialog — the deliberate second click of publishing: the editor's "Publish"
  button opens it showing the metadata about to go live (title, description, tags,
  who can see it); the author edits it and confirms.

  Presentational: it binds the metadata and renders server per-field errors inline;
  the page owns the publish request (see $lib/publish). On a 422 the page feeds
  `fieldErrors` back in and keeps the dialog open. Modal chrome is ConfirmDialog.
-->
<script lang="ts">
	import ConfirmDialog from './ConfirmDialog.svelte';
	import TagInput from './TagInput.svelte';
	import { accessLine, type FieldError } from '$lib/publish';

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

	let protectedTags = $state<string[]>([]);

	function errorsFor(field: string): string[] {
		return fieldErrors.filter((e) => e.field === field).map((e) => e.message);
	}

	// Errors not tied to an editable field (e.g. document too large).
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
		<TagInput bind:tags bind:protectedTags />
		{#each errorsFor('tags') as err (err)}
			<p class="error-text">{err}</p>
		{/each}
	</div>

	<p class="access" class:restricted={protectedTags.length > 0}>{accessLine(protectedTags)}</p>
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

	.access.restricted {
		color: var(--protected);
	}
</style>
