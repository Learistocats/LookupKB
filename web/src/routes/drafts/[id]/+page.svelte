<script lang="ts">
	import { enhance } from '$app/forms';
	import MarkdownEditor from '$lib/components/MarkdownEditor.svelte';
	import type { PageData, ActionData } from './$types';

	let { data, form }: { data: PageData; form: ActionData } = $props();

	// Seed the editable fields once from the draft loaded from Postgres. Reloading
	// the page re-runs `load` with a fresh component, so what shows here is always
	// what was last persisted (the round-trip proof).
	// svelte-ignore state_referenced_locally
	let title = $state(data.draft.title ?? '');
	// svelte-ignore state_referenced_locally
	let source = $state(data.draft.source_md ?? '');
	let saving = $state(false);
</script>

<svelte:head>
	<title>Draft · LookupKB</title>
</svelte:head>

<main>
	<a class="back" href="/">← Home</a>
	<h1>Edit draft</h1>
	<p class="meta">
		<code>{data.draft.slug}</code> · {data.draft.status}
	</p>

	<form
		method="POST"
		action="?/save"
		use:enhance={() => {
			saving = true;
			return async ({ update }) => {
				await update({ reset: false });
				saving = false;
			};
		}}
	>
		<label>
			Title
			<input name="title" bind:value={title} placeholder="Untitled draft" />
		</label>
		<MarkdownEditor bind:value={source} name="source_md" label="Markdown source" />
		<div class="actions">
			<button type="submit" disabled={saving}>{saving ? 'Saving…' : 'Save'}</button>
			{#if form?.saved}
				<span class="badge ok">Saved</span>
			{:else if form?.message}
				<span class="badge error">{form.message}</span>
			{/if}
		</div>
	</form>
</main>

<style>
	:global(body) {
		margin: 0;
		font-family:
			system-ui,
			-apple-system,
			'Segoe UI',
			Roboto,
			sans-serif;
		background: #0b0c0e;
		color: #e8eaed;
	}

	main {
		max-width: 48rem;
		margin: 0 auto;
		padding: 3rem 1.5rem;
	}

	.back {
		color: #93c5fd;
		text-decoration: none;
		font-size: 0.9rem;
	}

	h1 {
		margin: 0.75rem 0 0;
		font-size: 1.75rem;
		letter-spacing: -0.02em;
	}

	.meta {
		color: #9aa0a6;
		font-size: 0.9rem;
	}

	form {
		margin-top: 1.5rem;
		display: grid;
		gap: 1rem;
	}

	label {
		display: grid;
		gap: 0.4rem;
		color: #9aa0a6;
		font-size: 0.85rem;
		text-transform: uppercase;
		letter-spacing: 0.06em;
	}

	input {
		width: 100%;
		box-sizing: border-box;
		padding: 0.6rem 0.75rem;
		border: 1px solid #2a2e35;
		border-radius: 0.5rem;
		background: #131519;
		color: #e8eaed;
		font: inherit;
	}

	.actions {
		display: flex;
		align-items: center;
		gap: 1rem;
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

	.badge {
		display: inline-block;
		padding: 0.25rem 0.6rem;
		border-radius: 999px;
		font-weight: 600;
		font-size: 0.85rem;
	}

	.badge.ok {
		background: #0f2e1a;
		color: #4ade80;
	}

	.badge.error {
		background: #2e1113;
		color: #f87171;
	}
</style>
