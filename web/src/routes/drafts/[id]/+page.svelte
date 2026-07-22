<script lang="ts">
	import { enhance } from '$app/forms';
	import { onMount } from 'svelte';
	import { beforeNavigate } from '$app/navigation';
	import MarkdownEditor from '$lib/components/MarkdownEditor.svelte';
	import { createAutosave, type Autosave, type AutosaveState } from '$lib/autosave';
	import type { PageData, ActionData } from './$types';

	let { data, form }: { data: PageData; form: ActionData } = $props();

	// Seed the editable fields once from the draft loaded from Postgres. Reloading
	// the page re-runs `load` with a fresh component, so what shows here is always
	// what was last persisted (the round-trip proof).
	// svelte-ignore state_referenced_locally
	let title = $state(data.draft.title ?? '');
	// svelte-ignore state_referenced_locally
	let source = $state(data.draft.source_md ?? '');

	type Snapshot = { title: string | null; source_md: string | null };

	// Client autosave (AC-2.6). The no-JS <form> below still works on its own; once
	// hydrated, edits stream into the scheduler and the indicator reflects the save
	// lifecycle. Seeded idle; hydration wires the real scheduler.
	let save = $state<AutosaveState>({ status: 'idle', lastSavedAt: null });
	let autosaver: Autosave<Snapshot> | undefined;
	let hydrated = $state(false);
	let armed = false; // skip the first post-hydration effect run (the seed, not an edit)
	let nowTick = $state(Date.now());

	function snapshot(): Snapshot {
		return {
			title: title.length ? title : null,
			source_md: source.length ? source : null
		};
	}

	// Persist via the same-origin autosave proxy (attaches the bearer server-side).
	// Throwing on a non-OK response is what drives the scheduler's `error` state — a
	// false "saved" would silently lose data.
	async function persist(snap: Snapshot): Promise<void> {
		const res = await fetch(`/drafts/${data.draft.id}/save`, {
			method: 'PUT',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify(snap)
		});
		if (!res.ok) throw new Error(`save failed (${res.status})`);
	}

	onMount(() => {
		autosaver = createAutosave<Snapshot>({ save: persist, onState: (s) => (save = s) });
		hydrated = true;

		// Keep the "saved Xs ago" label live.
		const tick = setInterval(() => (nowTick = Date.now()), 1000);
		// Flush the last keystrokes if the tab is closed mid-edit.
		const onUnload = () => void autosaver?.flush();
		window.addEventListener('beforeunload', onUnload);

		return () => {
			clearInterval(tick);
			window.removeEventListener('beforeunload', onUnload);
			autosaver?.destroy();
		};
	});

	// Stream edits into the scheduler. Reading snapshot() establishes reactive deps
	// on title+source so this re-runs on every keystroke; `armed` skips the single
	// seed run so simply opening a draft never marks it dirty.
	$effect(() => {
		const snap = snapshot();
		if (!hydrated || !autosaver) return;
		if (!armed) {
			armed = true;
			return;
		}
		autosaver.update(snap);
	});

	// Don't lose the last few keystrokes on a fast route-away.
	beforeNavigate(() => void autosaver?.flush());

	function relativeSaved(at: number, now: number): string {
		const s = Math.max(0, Math.floor((now - at) / 1000));
		if (s < 3) return 'just now';
		if (s < 60) return `${s}s ago`;
		const m = Math.floor(s / 60);
		return `${m}m ago`;
	}

	// The human-readable indicator, derived from the save lifecycle (never a timer).
	let indicator = $derived.by(() => {
		switch (save.status) {
			case 'saving':
				return { text: 'Saving…', cls: 'saving' };
			case 'dirty':
				return { text: 'Unsaved changes', cls: 'dirty' };
			case 'error':
				return { text: 'Couldn’t save — retry', cls: 'error' };
			case 'saved':
				return { text: `Saved ${relativeSaved(save.lastSavedAt ?? nowTick, nowTick)}`, cls: 'ok' };
			default:
				return save.lastSavedAt
					? { text: `Saved ${relativeSaved(save.lastSavedAt, nowTick)}`, cls: 'ok' }
					: { text: 'No changes yet', cls: 'idle' };
		}
	});
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
		use:enhance={({ cancel }) => {
			// Hydrated: autosave already owns persistence — the button becomes a manual
			// flush (and a retry after an error). No-JS: fall through to the POST action.
			if (hydrated) {
				cancel();
				void autosaver?.flush();
				return;
			}
			return async ({ update }) => {
				await update({ reset: false });
			};
		}}
	>
		<label>
			Title
			<input name="title" bind:value={title} placeholder="Untitled draft" />
		</label>
		<MarkdownEditor bind:value={source} name="source_md" label="Markdown source" />
		<div class="actions">
			<button type="submit" disabled={save.status === 'saving'}>Save now</button>
			{#if hydrated}
				<span class="badge {indicator.cls}" aria-live="polite">{indicator.text}</span>
			{:else if form?.saved}
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

	.badge.saving {
		background: #1c2333;
		color: #93c5fd;
	}

	.badge.dirty {
		background: #2e2410;
		color: #fbbf24;
	}

	.badge.idle {
		background: #1a1d22;
		color: #9aa0a6;
	}
</style>
