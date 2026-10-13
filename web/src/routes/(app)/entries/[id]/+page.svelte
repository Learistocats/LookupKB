<script lang="ts">
	import { enhance } from '$app/forms';
	import { onMount } from 'svelte';
	import { beforeNavigate, goto, invalidate } from '$app/navigation';
	import Icon from '$lib/components/Icon.svelte';
	import MarkdownEditor from '$lib/components/MarkdownEditor.svelte';
	import PublishDialog from '$lib/components/PublishDialog.svelte';
	import { createAutosave, type Autosave, type AutosaveState } from '$lib/autosave';
	import { publishDraft, unpublishDraft, type FieldError } from '$lib/publish';
	import type { PageData, ActionData } from './$types';

	let { data, form }: { data: PageData; form: ActionData } = $props();

	// Seed the editable fields once from the working copy loaded from Postgres.
	// svelte-ignore state_referenced_locally
	let title = $state(data.draft.title ?? '');
	// svelte-ignore state_referenced_locally
	let description = $state(data.draft.description ?? '');
	// svelte-ignore state_referenced_locally
	let source = $state(data.draft.source_md ?? '');
	// svelte-ignore state_referenced_locally
	let status = $state(data.draft.status);
	// svelte-ignore state_referenced_locally
	let slug = $state(data.draft.slug);
	// Tags attached at the last publish prefill the (re-)publish dialog.
	// svelte-ignore state_referenced_locally
	let tags = $state<string[]>(data.draft.tags ?? []);

	// Publish confirm flow (F5): the button opens the dialog, "Publish"/"Update"
	// inside it is the deliberate second click. The server re-validates everything.
	let publishOpen = $state(false);
	let publishing = $state(false);
	let fieldErrors = $state<FieldError[]>([]);
	let publishError = $state('');
	let justPublished = $state(false);

	const isPublished = $derived(status === 'published');

	function openPublish() {
		fieldErrors = [];
		publishError = '';
		publishOpen = true;
	}

	async function confirmPublish() {
		publishing = true;
		publishError = '';
		try {
			// Publish renders the server-held working copy, so flush pending edits first.
			await autosaver?.flush();
			const outcome = await publishDraft(data.draft.id, { title, description, tags });
			if (outcome.ok) {
				status = outcome.doc.status;
				slug = outcome.doc.slug;
				fieldErrors = [];
				publishOpen = false;
				justPublished = true;
				void invalidate('app:entries');
			} else {
				fieldErrors = outcome.fieldErrors;
			}
		} catch (e) {
			publishError = e instanceof Error ? e.message : 'Publishing failed. Try again.';
		} finally {
			publishing = false;
		}
	}

	async function unpublish() {
		publishing = true;
		publishError = '';
		try {
			const doc = await unpublishDraft(data.draft.id);
			status = doc.status;
			justPublished = false;
			void invalidate('app:entries');
		} catch (e) {
			publishError = e instanceof Error ? e.message : 'Unpublishing failed. Try again.';
		} finally {
			publishing = false;
		}
	}

	type Snapshot = { title: string | null; description: string | null; source_md: string | null };

	// Client autosave (AC-2.6). The no-JS <form> still works on its own; once
	// hydrated, edits stream into the scheduler and the indicator follows it.
	let save = $state<AutosaveState>({ status: 'idle', lastSavedAt: null });
	let autosaver: Autosave<Snapshot> | undefined;
	let hydrated = $state(false);
	let armed = false; // skip the first post-hydration effect run (the seed, not an edit)
	let nowTick = $state(Date.now());

	function snapshot(): Snapshot {
		return {
			title: title.length ? title : null,
			description: description.length ? description : null,
			source_md: source.length ? source : null
		};
	}

	// Persist via the same-origin proxy (attaches the bearer server-side). Any
	// status saves: on a published entry this edits the working copy, and readers
	// keep seeing the published version until "Update". Throwing on non-OK drives
	// the scheduler's error state — a false "saved" would silently lose data.
	async function persist(snap: Snapshot): Promise<void> {
		const res = await fetch(`/entries/${data.draft.id}/save`, {
			method: 'PUT',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify(snap)
		});
		if (!res.ok) throw new Error(`save failed (${res.status})`);
		justPublished = false;
		void invalidate('app:entries');
	}

	onMount(() => {
		autosaver = createAutosave<Snapshot>({ save: persist, onState: (s) => (save = s) });
		hydrated = true;
		const tick = setInterval(() => (nowTick = Date.now()), 1000);
		const onUnload = () => void autosaver?.flush();
		window.addEventListener('beforeunload', onUnload);
		return () => {
			clearInterval(tick);
			window.removeEventListener('beforeunload', onUnload);
			autosaver?.destroy();
		};
	});

	// Stream edits into the scheduler; `armed` skips the seed run so merely opening
	// an entry never marks it dirty.
	$effect(() => {
		const snap = snapshot();
		if (!hydrated || !autosaver) return;
		if (!armed) {
			armed = true;
			return;
		}
		autosaver.update(snap);
	});

	// Never lose the last keystrokes on a route change. With edits pending, hold the
	// navigation until the save lands, then continue: navigating while the save is in
	// flight would let its panel refresh (invalidate) cancel the navigation.
	beforeNavigate((nav) => {
		if (!autosaver) return;
		const pending = save.status === 'dirty' || save.status === 'saving';
		if (!pending || nav.type === 'leave' || !nav.to) {
			void autosaver.flush();
			return;
		}
		nav.cancel();
		const to = nav.to.url;
		void autosaver.flush().finally(() => {
			if (to.origin === location.origin) void goto(to);
			else location.href = to.href;
		});
	});

	function relativeSaved(at: number, now: number): string {
		const s = Math.max(0, Math.floor((now - at) / 1000));
		if (s < 3) return 'just now';
		if (s < 60) return `${s}s ago`;
		return `${Math.floor(s / 60)}m ago`;
	}

	let indicator = $derived.by(() => {
		switch (save.status) {
			case 'saving':
				return { text: 'Saving…', cls: 'saving' };
			case 'dirty':
				return { text: 'Unsaved changes', cls: 'dirty' };
			case 'error':
				return { text: 'Couldn’t save — retrying', cls: 'error' };
			case 'saved':
				return { text: `Saved ${relativeSaved(save.lastSavedAt ?? nowTick, nowTick)}`, cls: 'ok' };
			default:
				return save.lastSavedAt
					? { text: `Saved ${relativeSaved(save.lastSavedAt, nowTick)}`, cls: 'ok' }
					: { text: 'All changes saved', cls: 'idle' };
		}
	});

	const statusChip = $derived(
		status === 'draft'
			? { text: 'Draft', cls: 'chip-draft' }
			: status === 'published'
				? { text: 'Published', cls: 'chip-ok' }
				: { text: 'Unpublished', cls: '' }
	);
</script>

<svelte:head>
	<title>{title || 'Untitled'} · LookupKB</title>
</svelte:head>

<header class="topbar">
	<div class="state">
		<span class="chip {statusChip.cls}">{statusChip.text}</span>
		{#if justPublished}
			<span class="saved ok" aria-live="polite">Live — readers see this version</span>
		{:else if hydrated}
			<span class="saved {indicator.cls}" aria-live="polite">{indicator.text}</span>
		{:else if form?.saved}
			<span class="saved ok">Saved</span>
		{:else if form?.message}
			<span class="saved error">{form.message}</span>
		{/if}
	</div>
	<div class="actions">
		{#if publishError}
			<span class="error-text">{publishError}</span>
		{/if}
		{#if hydrated}
			{#if isPublished}
				<a class="btn btn-ghost" href={`/docs/${slug}`}>
					<Icon name="eye" size={16} /> View
				</a>
				<button
					type="button"
					class="btn btn-ghost btn-danger"
					onclick={unpublish}
					disabled={publishing}
				>
					{publishing ? 'Working…' : 'Unpublish'}
				</button>
				<button type="button" class="btn btn-primary" onclick={openPublish} disabled={publishing}>
					Update
				</button>
			{:else}
				<button type="button" class="btn btn-primary" onclick={openPublish} disabled={publishing}>
					Publish…
				</button>
			{/if}
		{/if}
	</div>
</header>

<form
	class="page"
	method="POST"
	action="?/save"
	use:enhance={({ cancel }) => {
		// Hydrated: autosave owns persistence, so a submit (Enter in a field) just flushes.
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
	<label class="visually-hidden" for="title">Title</label>
	<input
		id="title"
		class="title"
		name="title"
		bind:value={title}
		placeholder="Untitled"
		autocomplete="off"
	/>
	<label class="visually-hidden" for="description">Description</label>
	<input
		id="description"
		class="description"
		name="description"
		bind:value={description}
		placeholder="Add a one-line description"
		autocomplete="off"
	/>

	<MarkdownEditor
		bind:value={source}
		name="source_md"
		label="Content (Markdown)"
		placeholder="Start writing in Markdown…"
	/>

	{#if !hydrated}
		<div class="nojs">
			<button class="btn" type="submit">Save</button>
		</div>
	{/if}
</form>

{#if hydrated}
	<PublishDialog
		bind:open={publishOpen}
		bind:title
		bind:description
		bind:tags
		{fieldErrors}
		busy={publishing}
		confirmLabel={isPublished ? 'Update' : 'Publish'}
		onConfirm={confirmPublish}
		onCancel={() => (publishOpen = false)}
	/>
{/if}

<style>
	.topbar {
		position: sticky;
		top: 0;
		z-index: 10;
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 1rem;
		height: 3.5rem;
		padding: 0 1.25rem 0 2.5rem;
		background: color-mix(in srgb, var(--bg) 88%, transparent);
		backdrop-filter: blur(8px);
		border-bottom: 1px solid var(--border);
	}

	.state,
	.actions {
		display: flex;
		align-items: center;
		gap: 0.6rem;
		min-width: 0;
	}

	.saved {
		color: var(--fg-subtle);
		font-size: var(--text-sm);
		white-space: nowrap;
	}

	.saved.error {
		color: var(--danger);
	}

	.saved.dirty,
	.saved.saving {
		color: var(--fg-muted);
	}

	.page {
		max-width: 46rem;
		margin: 0 auto;
		padding: 3rem 1.5rem 6rem;
		display: grid;
		gap: 0.25rem;
	}

	.title,
	.description {
		width: 100%;
		border: none;
		background: transparent;
		outline: none;
		padding: 0;
	}

	.title {
		font-size: var(--text-2xl);
		font-weight: 700;
		letter-spacing: -0.025em;
		line-height: 1.2;
	}

	.description {
		margin-bottom: 1.5rem;
		color: var(--fg-muted);
		font-size: var(--text-md);
	}

	.title::placeholder,
	.description::placeholder {
		color: var(--fg-subtle);
	}

	.nojs {
		margin-top: 1rem;
	}

	@media (max-width: 600px) {
		.topbar {
			padding-left: 2.25rem;
		}

		.saved {
			display: none;
		}

		.page {
			padding-top: 2rem;
		}

		.title {
			font-size: var(--text-xl);
		}
	}
</style>
