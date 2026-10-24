<script lang="ts">
	import { enhance } from '$app/forms';
	import { onMount } from 'svelte';
	import { beforeNavigate, goto, invalidate, replaceState } from '$app/navigation';
	import Icon from '$lib/components/Icon.svelte';
	import MarkdownEditor from '$lib/components/MarkdownEditor.svelte';
	import PublishDialog from '$lib/components/PublishDialog.svelte';
	import DeleteEntryDialog from '$lib/components/DeleteEntryDialog.svelte';
	import { createAutosave, saveIndicator, type Autosave, type AutosaveState } from '$lib/autosave';
	import { now } from '$lib/time';
	import { publishDraft, unpublishDraft, type FieldError } from '$lib/publish';
	import { isBlankEntry, type WorkingCopy } from '$lib/entries';
	import { canDeleteEntry } from '$lib/drafts';
	import type { PageData, ActionData } from './$types';

	let { data, form }: { data: PageData; form: ActionData } = $props();

	// Seeded once: the layout remounts this page per entry. A new entry
	// (`/entries/new`) has no row yet; `id` stays null until its first non-blank save.
	// svelte-ignore state_referenced_locally
	const seed = data.draft;
	const entry = $state({
		id: seed?.id ?? null,
		title: seed?.title ?? '',
		description: seed?.description ?? '',
		source: seed?.source_md ?? '',
		status: seed?.status ?? 'draft',
		slug: seed?.slug ?? '',
		tags: seed?.tags ?? []
	} as {
		id: string | null;
		title: string;
		description: string;
		source: string;
		status: string;
		slug: string;
		tags: string[];
	});

	let publishOpen = $state(false);
	let publishing = $state(false);
	let fieldErrors = $state<FieldError[]>([]);
	let publishError = $state('');
	let justPublished = $state(false);

	const isPublished = $derived(entry.status === 'published');

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
			if (!entry.id) {
				publishError = 'Write something before publishing.';
				return;
			}
			const outcome = await publishDraft(entry.id, {
				title: entry.title,
				description: entry.description,
				tags: entry.tags
			});
			if (outcome.ok) {
				entry.status = outcome.doc.status;
				entry.slug = outcome.doc.slug;
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
			if (!entry.id) return;
			entry.status = (await unpublishDraft(entry.id)).status;
			justPublished = false;
			void invalidate('app:entries');
		} catch (e) {
			publishError = e instanceof Error ? e.message : 'Unpublishing failed. Try again.';
		} finally {
			publishing = false;
		}
	}

	let deleteOpen = $state(false);
	let deleted = false;
	const deletable = $derived(canDeleteEntry(entry.id, entry.status));

	// Tear autosave down first so neither a pending save nor the leave-flush hits the gone row.
	async function onDeleted() {
		deleted = true;
		autosaver?.destroy();
		await goto('/');
	}

	let save = $state<AutosaveState>({ status: 'idle', lastSavedAt: null });
	let autosaver: Autosave<WorkingCopy> | undefined;
	let hydrated = $state(false);
	let armed = false; // skip the first post-hydration effect run (the seed, not an edit)
	let savedTitle = seed?.title ?? null; // the history panel lists titles

	function snapshot(): WorkingCopy {
		return {
			title: entry.title.length ? entry.title : null,
			description: entry.description.length ? entry.description : null,
			source_md: entry.source.length ? entry.source : null
		};
	}

	// Saves via the same-origin proxy (which attaches the bearer). Any status saves:
	// on a published entry this edits the working copy until "Update". Throwing on
	// non-OK drives the scheduler's error state; a false "saved" would lose data.
	async function persist(snap: WorkingCopy): Promise<void> {
		if (!entry.id && isBlankEntry(snap)) return;
		const res = await fetch(`/entries/${entry.id ?? 'new'}/save`, {
			method: 'PUT',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify(snap)
		});
		if (!res.ok) throw new Error(`save failed (${res.status})`);
		const created = !entry.id;
		if (created) {
			entry.id = ((await res.json()) as { id: string }).id;
			// Swap the URL without reloading the editor.
			replaceState(`/entries/${entry.id}`, {});
		}
		justPublished = false;
		if (created || snap.title !== savedTitle) void invalidate('app:entries');
		savedTitle = snap.title;
	}

	onMount(() => {
		autosaver = createAutosave<WorkingCopy>({ save: persist, onState: (s) => (save = s) });
		hydrated = true;
		const onUnload = () => void autosaver?.flush();
		window.addEventListener('beforeunload', onUnload);
		return () => {
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
	// navigation until the save lands: navigating mid-save would let its panel refresh
	// (invalidate) cancel the navigation.
	beforeNavigate((nav) => {
		if (!autosaver || deleted) return;
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

	const indicator = $derived(saveIndicator(save, !!entry.id, now()));

	const statusChip = $derived(
		!entry.id
			? { text: 'New', cls: '' }
			: entry.status === 'draft'
				? { text: 'Draft', cls: 'chip-draft' }
				: entry.status === 'published'
					? { text: 'Published', cls: 'chip-ok' }
					: { text: 'Unpublished', cls: '' }
	);
</script>

<svelte:head>
	<title>{entry.title || 'Untitled'} · LookupKB</title>
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
			{#if deletable}
				<button
					type="button"
					class="btn btn-ghost btn-danger"
					onclick={() => (deleteOpen = true)}
					disabled={publishing}
				>
					<Icon name="trash" size={16} /> Delete
				</button>
			{/if}
			{#if isPublished}
				<a class="btn btn-ghost" href={`/docs/${entry.slug}`}>
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
	class="page page-column"
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
		bind:value={entry.title}
		placeholder="Untitled"
		autocomplete="off"
	/>
	<label class="visually-hidden" for="description">Description</label>
	<input
		id="description"
		class="description"
		name="description"
		bind:value={entry.description}
		placeholder="Add a one-line description"
		autocomplete="off"
	/>

	<MarkdownEditor
		bind:value={entry.source}
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
		bind:title={entry.title}
		bind:description={entry.description}
		bind:tags={entry.tags}
		{fieldErrors}
		busy={publishing}
		confirmLabel={isPublished ? 'Update' : 'Publish'}
		onConfirm={confirmPublish}
		onCancel={() => (publishOpen = false)}
	/>
	<DeleteEntryDialog bind:open={deleteOpen} id={entry.id} {onDeleted} />
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
