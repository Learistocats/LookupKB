<!--
  Sidebar — the history panel: the caller's own entries, drafts pinned on top, then
  everything else by last edit. Collapsed by default and toggled by the handle on the
  left edge (or ⌘/Ctrl + \). On narrow screens it overlays the page with a scrim and
  closes after navigating. Draft and unpublished items can be deleted behind a confirm.
-->
<script lang="ts">
	import { page } from '$app/state';
	import { afterNavigate, goto } from '$app/navigation';
	import Icon from './Icon.svelte';
	import DeleteEntryDialog from './DeleteEntryDialog.svelte';
	import { canDeleteEntry } from '$lib/drafts';
	import { now, relativeTime } from '$lib/time';
	import { PANEL_COOKIE, applyTheme, nextTheme, setPrefCookie, type ThemePref } from '$lib/theme';
	import type { EntrySummary } from '$lib/entries';

	let {
		entries,
		email,
		isSuperAdmin = false,
		theme = $bindable<ThemePref>('system'),
		open = $bindable(false)
	}: {
		entries: EntrySummary[];
		email: string | null;
		isSuperAdmin?: boolean;
		theme?: ThemePref;
		open?: boolean;
	} = $props();

	const NARROW = '(max-width: 899px)';

	const drafts = $derived(entries.filter((e) => e.status === 'draft'));
	const others = $derived(entries.filter((e) => e.status !== 'draft'));

	const themeLabel: Record<ThemePref, string> = {
		system: 'System theme',
		light: 'Light theme',
		dark: 'Dark theme'
	};
	const themeIcon = { system: 'monitor', light: 'sun', dark: 'moon' } as const;

	function setOpen(v: boolean) {
		open = v;
		setPrefCookie(PANEL_COOKIE, v ? 1 : 0);
	}

	function cycleTheme() {
		theme = nextTheme(theme);
		applyTheme(theme);
	}

	function href(e: EntrySummary): string {
		return e.status === 'published' ? `/docs/${e.slug}` : `/entries/${e.id}`;
	}

	function isActive(e: EntrySummary): boolean {
		const p = page.url.pathname;
		return p === `/entries/${e.id}` || p === `/docs/${e.slug}`;
	}

	let pendingDelete = $state<EntrySummary | null>(null);
	let confirmOpen = $state(false);

	function askDelete(e: EntrySummary) {
		pendingDelete = e;
		confirmOpen = true;
	}

	async function onDeleted() {
		const wasOpen = pendingDelete && isActive(pendingDelete);
		pendingDelete = null;
		if (wasOpen) await goto('/');
	}

	function onKeydown(ev: KeyboardEvent) {
		if ((ev.metaKey || ev.ctrlKey) && ev.key === '\\') {
			ev.preventDefault();
			setOpen(!open);
		} else if (ev.key === 'Escape' && open && !confirmOpen && window.matchMedia(NARROW).matches) {
			setOpen(false);
		}
	}

	// On small screens the panel covers the page, so close it once a link is followed.
	afterNavigate(({ type }) => {
		if (type !== 'enter' && window.matchMedia(NARROW).matches) setOpen(false);
	});
</script>

<svelte:window onkeydown={onKeydown} />

{#snippet row(e: EntrySummary)}
	<li>
		<a href={href(e)} class:active={isActive(e)} aria-current={isActive(e) ? 'page' : undefined}>
			<span class="title" class:untitled={!e.title}>{e.title || 'Untitled'}</span>
			{#if e.status === 'draft'}
				<span class="chip chip-draft">Draft</span>
			{:else if e.status === 'unpublished'}
				<span class="chip">Unpublished</span>
			{:else}
				<time class="when" datetime={e.updated_at}>{relativeTime(e.updated_at, now())}</time>
			{/if}
		</a>
		{#if canDeleteEntry(e.id, e.status)}
			<button
				class="icon-btn item-delete"
				type="button"
				title="Delete"
				aria-label={`Delete ${e.title || 'Untitled'}`}
				onclick={() => askDelete(e)}
			>
				<Icon name="trash" size={15} />
			</button>
		{/if}
	</li>
{/snippet}

{#if open}
	<button class="scrim" type="button" aria-label="Close panel" onclick={() => setOpen(false)}
	></button>
{/if}

<aside id="history-panel" class="panel" class:open aria-label="Your entries" inert={!open}>
	<div class="panel-head">
		<a class="wordmark" href="/">LookupKB</a>
	</div>

	<div class="new-entry">
		<a class="btn new" href="/entries/new">
			<Icon name="plus" size={16} />
			New entry
		</a>
	</div>

	<nav class="lists">
		{#if drafts.length > 0}
			<section>
				<h2 class="eyebrow">Drafts</h2>
				<ul>
					{#each drafts as e (e.id)}
						{@render row(e)}
					{/each}
				</ul>
			</section>
		{/if}

		<section>
			<h2 class="eyebrow">Entries</h2>
			{#if others.length === 0}
				<p class="empty">Published entries you write show up here.</p>
			{:else}
				<ul>
					{#each others as e (e.id)}
						{@render row(e)}
					{/each}
				</ul>
			{/if}
		</section>
	</nav>

	{#if isSuperAdmin}
		<div class="admin-link">
			<a href="/admin" class:active={page.url.pathname.startsWith('/admin')}>Admin</a>
		</div>
	{/if}

	<div class="panel-foot">
		<span class="email" title={email ?? ''}>{email ?? 'Not signed in'}</span>
		<a
			class="icon-btn"
			href="/settings"
			title="Settings"
			aria-label="Settings"
			aria-current={page.url.pathname === '/settings' ? 'page' : undefined}
		>
			<Icon name="settings" size={17} />
		</a>
		<button
			class="icon-btn"
			type="button"
			onclick={cycleTheme}
			title={themeLabel[theme]}
			aria-label={`${themeLabel[theme]} (change)`}
		>
			<Icon name={themeIcon[theme]} size={17} />
		</button>
		<form method="POST" action="/auth/logout">
			<button class="icon-btn" type="submit" title="Sign out" aria-label="Sign out">
				<Icon name="log-out" size={17} />
			</button>
		</form>
	</div>
</aside>

<DeleteEntryDialog bind:open={confirmOpen} id={pendingDelete?.id} {onDeleted} />

<button
	class="handle"
	class:open
	type="button"
	aria-controls="history-panel"
	aria-expanded={open}
	aria-label={open ? 'Hide your entries' : 'Show your entries'}
	title={open ? 'Hide entries (⌘\\)' : 'Show entries (⌘\\)'}
	onclick={() => setOpen(!open)}
>
	<Icon name={open ? 'chevron-left' : 'chevron-right'} size={16} />
</button>

<style>
	.panel {
		position: fixed;
		inset: 0 auto 0 0;
		z-index: 30;
		width: var(--panel-width);
		display: flex;
		flex-direction: column;
		background: var(--surface);
		border-right: 1px solid var(--border);
		transform: translateX(-100%);
		transition: transform 220ms var(--ease);
	}

	.panel.open {
		transform: none;
	}

	.panel-head {
		padding: 1rem 1rem 0.5rem;
	}

	.wordmark {
		color: var(--fg);
		font-weight: 700;
		font-size: var(--text-md);
		letter-spacing: -0.01em;
	}

	.wordmark:hover {
		text-decoration: none;
	}

	.new-entry {
		padding: 0.5rem 0.75rem 0.75rem;
	}

	.new {
		width: 100%;
		justify-content: flex-start;
	}

	.lists {
		flex: 1;
		overflow-y: auto;
		padding: 0 0.5rem 1rem;
		display: grid;
		/* minmax(0, 1fr): without it the auto track grows to the rows' unwrapped
		   title width and pushes the row's chip/delete button past the panel edge. */
		grid-template-columns: minmax(0, 1fr);
		align-content: start;
		gap: 1.25rem;
	}

	h2 {
		margin: 0 0 0.25rem;
		padding: 0 0.5rem;
	}

	ul {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		grid-template-columns: minmax(0, 1fr);
		gap: 1px;
	}

	li {
		display: flex;
		align-items: center;
		gap: 0.25rem;
		min-width: 0;
		border-radius: var(--radius-sm);
	}

	li:hover {
		background: var(--surface-hover);
	}

	li:has(a.active) {
		background: var(--accent-soft);
	}

	li a {
		flex: 1;
		min-width: 0;
		display: flex;
		align-items: center;
		gap: 0.5rem;
		min-height: 2.125rem;
		padding: 0.3rem 0.5rem;
		border-radius: var(--radius-sm);
		color: var(--fg);
		font-size: var(--text-sm);
	}

	li a:hover {
		text-decoration: none;
	}

	li .chip {
		flex: none;
	}

	/* Revealed on row hover/focus. */
	.item-delete {
		flex: none;
		width: 1.75rem;
		height: 1.75rem;
		margin-right: 0.25rem;
		opacity: 0;
		transition: opacity 120ms var(--ease);
	}

	li:hover .item-delete,
	li:focus-within .item-delete {
		opacity: 1;
	}

	.item-delete:hover {
		color: var(--danger);
		background: var(--danger-soft);
	}

	@media (hover: none) {
		.item-delete {
			opacity: 1;
		}
	}

	.title {
		flex: 1;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.untitled {
		color: var(--fg-subtle);
		font-style: italic;
	}

	.when {
		color: var(--fg-subtle);
		font-size: var(--text-xs);
		font-variant-numeric: tabular-nums;
	}

	.empty {
		padding: 0 0.5rem;
	}

	.admin-link {
		padding: 0 0.5rem 0.5rem;
	}

	.admin-link a {
		display: flex;
		align-items: center;
		min-height: 2.125rem;
		padding: 0.3rem 0.5rem;
		border-radius: var(--radius-sm);
		color: var(--fg-muted);
		font-size: var(--text-sm);
	}

	.admin-link a:hover {
		background: var(--surface-hover);
		color: var(--fg);
		text-decoration: none;
	}

	.admin-link a.active {
		background: var(--accent-soft);
		color: var(--fg);
	}

	.panel-foot {
		display: flex;
		align-items: center;
		gap: 0.25rem;
		padding: 0.6rem 0.5rem 0.6rem 1rem;
		border-top: 1px solid var(--border);
	}

	.email {
		flex: 1;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		color: var(--fg-muted);
		font-size: var(--text-sm);
	}

	.handle {
		position: fixed;
		top: 50%;
		left: 0;
		z-index: 31;
		display: grid;
		place-items: center;
		width: 1.5rem;
		height: 3.5rem;
		padding: 0;
		border: 1px solid var(--border);
		border-left: none;
		border-radius: 0 var(--radius) var(--radius) 0;
		background: var(--surface);
		color: var(--fg-muted);
		box-shadow: var(--shadow-sm);
		cursor: pointer;
		transform: translateY(-50%);
		transition:
			left 220ms var(--ease),
			color 120ms var(--ease),
			background 120ms var(--ease);
	}

	.handle:hover {
		color: var(--fg);
		background: var(--surface-hover);
	}

	.handle.open {
		left: var(--panel-width);
	}

	.scrim {
		display: none;
	}

	@media (max-width: 899px) {
		.panel {
			width: min(var(--panel-width), 85vw);
			box-shadow: var(--shadow-md);
		}

		.handle.open {
			left: min(var(--panel-width), 85vw);
		}

		.scrim {
			display: block;
			position: fixed;
			inset: 0;
			z-index: 29;
			border: none;
			background: var(--scrim);
		}
	}
</style>
