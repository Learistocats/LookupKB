<script lang="ts">
	import { enhance } from '$app/forms';
	import { invalidate } from '$app/navigation';
	import AdminConfirmDialog from '$lib/components/admin/AdminConfirmDialog.svelte';
	import { grantTag, revokeTag } from '$lib/admin';
	import { debounce, searchTags, type TagSuggestion } from '$lib/tags';
	import type { ActionData, PageData } from './$types';

	let { data, form }: { data: PageData; form: ActionData } = $props();

	const group = $derived(data.group);

	// --- confirmation dialog, exposed to $lib/admin as a Promise-returning `ask` ---
	let confirmOpen = $state(false);
	let confirmTitle = $state('Are you sure?');
	let confirmMessage = $state('');
	let confirmLabel = $state('Confirm');
	let confirmDanger = $state(false);
	let settle: ((ok: boolean) => void) | null = null;

	function ask(message: string, opts: { title?: string; label?: string; danger?: boolean } = {}) {
		confirmTitle = opts.title ?? 'Change tag protection?';
		confirmLabel = opts.label ?? 'Yes, continue';
		confirmDanger = opts.danger ?? false;
		confirmMessage = message;
		confirmOpen = true;
		return new Promise<boolean>((resolve) => (settle = resolve));
	}

	function resolveConfirm(ok: boolean) {
		settle?.(ok);
		settle = null;
	}

	// --- tag policy (client-side so the 409 confirm-and-retry can run) ---
	let tagDraft = $state('');
	let tagBusy = $state(false);
	let tagError = $state('');
	let suggestions = $state<TagSuggestion[]>([]);

	const runSearch = debounce(async (q: string) => {
		if (!q.trim()) {
			suggestions = [];
			return;
		}
		try {
			const granted = new Set(group.tags.map((t) => t.slug));
			suggestions = (await searchTags(q)).filter((s) => !granted.has(s.slug));
		} catch {
			suggestions = []; // best-effort; typing a tag still works
		}
	}, 200);

	async function onGrant(e: SubmitEvent) {
		e.preventDefault();
		if (!tagDraft.trim() || tagBusy) return;
		tagBusy = true;
		tagError = '';
		try {
			const out = await grantTag(group.id, tagDraft, (m) => ask(m));
			if (out.status === 'done') {
				tagDraft = '';
				suggestions = [];
				await invalidate('admin:group');
			}
		} catch (err) {
			tagError = err instanceof Error ? err.message : 'Could not add the tag.';
		} finally {
			tagBusy = false;
		}
	}

	async function onRevoke(tagId: string) {
		if (tagBusy) return;
		tagBusy = true;
		tagError = '';
		try {
			const out = await revokeTag(group.id, tagId, (m) => ask(m));
			if (out.status === 'done') await invalidate('admin:group');
		} catch (err) {
			tagError = err instanceof Error ? err.message : 'Could not remove the tag.';
		} finally {
			tagBusy = false;
		}
	}

	// --- delete group: confirm, then submit the hidden form ---
	let deleteForm = $state<HTMLFormElement>();
	async function onDelete() {
		const ok = await ask(
			`Delete the group "${group.name}"? Its members and tag policy are removed too.`,
			{ title: 'Delete group?', label: 'Delete group', danger: true }
		);
		if (ok) deleteForm?.requestSubmit();
	}

	// The rename field starts from the current name and follows it after a save.
	let name = $derived(group.name);
</script>

<svelte:head><title>{group.name} · Admin · LookupKB</title></svelte:head>

<p class="back"><a href="/admin/groups">← All groups</a></p>

{#if form?.message}<p class="error-text" role="alert">{form.message}</p>{/if}

<section class="card">
	<h2>Group</h2>
	<form method="POST" action="?/rename" class="inline-form" use:enhance>
		<label class="visually-hidden" for="rename">Group name</label>
		<input id="rename" class="input" name="name" bind:value={name} required maxlength="100" />
		<button class="btn" type="submit" disabled={name.trim() === group.name}>Rename</button>
		<button class="btn btn-danger" type="button" onclick={onDelete}>Delete group</button>
	</form>
	<form method="POST" action="?/delete" bind:this={deleteForm} use:enhance hidden></form>
</section>

<section class="card">
	<h2>Members</h2>
	<form method="POST" action="?/addMember" class="inline-form" use:enhance>
		<label class="visually-hidden" for="member-email">Member email</label>
		<input
			id="member-email"
			class="input"
			type="email"
			name="email"
			required
			placeholder="person@example.com"
			autocomplete="off"
		/>
		<button class="btn btn-primary" type="submit">Add member</button>
	</form>
	{#if group.members.length === 0}
		<p class="empty">No members yet.</p>
	{:else}
		<ul class="rows">
			{#each group.members as m (m.id)}
				<li>
					<span class="grow">
						{m.email}
						{#if m.display_name}<span class="muted">· {m.display_name}</span>{/if}
					</span>
					<form method="POST" action="?/removeMember" use:enhance>
						<input type="hidden" name="user_id" value={m.id} />
						<button class="btn btn-ghost" type="submit" aria-label={`Remove ${m.email}`}
							>Remove</button
						>
					</form>
				</li>
			{/each}
		</ul>
	{/if}
</section>

<section class="card">
	<h2>Tag policy</h2>
	<p class="muted">
		Tags this group is authorized for. A tag granted to any group becomes a protected tag.
	</p>
	<form class="inline-form" onsubmit={onGrant}>
		<label class="visually-hidden" for="policy-tag">Tag</label>
		<input
			id="policy-tag"
			class="input"
			list="policy-tag-suggestions"
			bind:value={tagDraft}
			oninput={() => runSearch(tagDraft)}
			placeholder="Add a tag"
			autocomplete="off"
		/>
		<datalist id="policy-tag-suggestions">
			{#each suggestions as s (s.slug)}
				<option value={s.slug}></option>
			{/each}
		</datalist>
		<button class="btn btn-primary" type="submit" disabled={tagBusy || !tagDraft.trim()}
			>Add tag</button
		>
	</form>
	{#if tagError}<p class="error-text" role="alert">{tagError}</p>{/if}
	{#if group.tags.length === 0}
		<p class="empty">No tags in this group's policy.</p>
	{:else}
		<ul class="rows">
			{#each group.tags as t (t.tag_id)}
				<li>
					<span class="grow"><span class="chip">{t.label}</span></span>
					<button
						class="btn btn-ghost"
						type="button"
						disabled={tagBusy}
						onclick={() => onRevoke(t.tag_id)}
						aria-label={`Remove tag ${t.label}`}>Remove</button
					>
				</li>
			{/each}
		</ul>
	{/if}
</section>

<AdminConfirmDialog
	bind:open={confirmOpen}
	title={confirmTitle}
	message={confirmMessage}
	{confirmLabel}
	danger={confirmDanger}
	onConfirm={() => resolveConfirm(true)}
	onCancel={() => resolveConfirm(false)}
/>

<style>
	.back {
		margin: 0;
		font-size: var(--text-sm);
	}
</style>
