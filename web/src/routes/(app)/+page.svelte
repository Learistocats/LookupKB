<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import Icon from '$lib/components/Icon.svelte';
	import { relativeTime } from '$lib/time';
	import type { SearchHit, SearchResult } from '$lib/entries';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();

	let query = $state('');
	let hits = $state<SearchHit[]>([]);
	let searched = $state(''); // the query the current hits belong to
	let loading = $state(false);
	let failed = $state('');
	let active = $state(-1);
	let input = $state<HTMLInputElement>();
	// Shortcut hint for the search box (⌘K on Apple devices, Ctrl K elsewhere).
	let shortcut = $state('Ctrl K');

	const hasQuery = $derived(query.trim().length > 0);
	const list = $derived(hasQuery ? hits : data.recent);

	let timer: ReturnType<typeof setTimeout> | undefined;
	let inflight: AbortController | undefined;

	async function run(q: string) {
		inflight?.abort();
		const ctrl = new AbortController();
		inflight = ctrl;
		loading = true;
		try {
			const res = await fetch(`/search?q=${encodeURIComponent(q)}&limit=20`, {
				signal: ctrl.signal
			});
			const body = await res.json();
			if (!res.ok) throw new Error(body.error ?? 'Search is unavailable right now.');
			hits = (body as SearchResult).hits;
			searched = q;
			failed = '';
			active = hits.length ? 0 : -1;
		} catch (e) {
			if ((e as Error).name === 'AbortError') return;
			failed = e instanceof Error ? e.message : 'Search is unavailable right now.';
		} finally {
			if (inflight === ctrl) loading = false;
		}
	}

	function onInput() {
		clearTimeout(timer);
		const q = query.trim();
		if (!q) {
			inflight?.abort();
			hits = [];
			searched = '';
			failed = '';
			loading = false;
			active = -1;
			return;
		}
		timer = setTimeout(() => run(q), 150);
	}

	function onKeydown(e: KeyboardEvent) {
		if (e.key === 'ArrowDown' && list.length) {
			e.preventDefault();
			active = (active + 1) % list.length;
		} else if (e.key === 'ArrowUp' && list.length) {
			e.preventDefault();
			active = active <= 0 ? list.length - 1 : active - 1;
		} else if (e.key === 'Enter' && active >= 0 && list[active]) {
			e.preventDefault();
			goto(`/docs/${list[active].slug}`);
		} else if (e.key === 'Escape') {
			query = '';
			onInput();
		}
	}

	onMount(() => {
		if (/Mac|iPhone|iPad/.test(navigator.platform)) shortcut = '⌘K';
		input?.focus();
		const onSlash = (e: KeyboardEvent) => {
			const t = e.target as HTMLElement;
			if (e.key === '/' && !['INPUT', 'TEXTAREA'].includes(t.tagName) && !t.isContentEditable) {
				e.preventDefault();
				input?.focus();
			}
		};
		window.addEventListener('keydown', onSlash);
		return () => window.removeEventListener('keydown', onSlash);
	});
</script>

<svelte:head>
	<title>LookupKB</title>
</svelte:head>

<div class="home" class:searching={hasQuery}>
	<div class="column">
		<h1 class="greeting">What are you looking for?</h1>

		<div class="bar">
			<label class="search" for="q">
				<span class="search-icon"><Icon name="search" size={19} /></span>
				<span class="visually-hidden">Search the knowledge base</span>
				<input
					id="q"
					bind:this={input}
					bind:value={query}
					oninput={onInput}
					onkeydown={onKeydown}
					type="search"
					placeholder="Search titles, tags and content"
					autocomplete="off"
					spellcheck="false"
					role="combobox"
					aria-expanded={list.length > 0}
					aria-controls="results"
					aria-activedescendant={active >= 0 ? `hit-${active}` : undefined}
				/>
				{#if loading}
					<span class="spinner" aria-hidden="true"></span>
				{:else if !hasQuery}
					<kbd aria-hidden="true">{shortcut}</kbd>
				{/if}
			</label>
			<form method="POST" action="/entries/new">
				<button class="new" type="submit" title="New entry" aria-label="New entry">
					<Icon name="plus" size={22} />
				</button>
			</form>
		</div>

		<div class="results" aria-live="polite">
			{#if failed}
				<p class="notice">{failed}</p>
			{:else if hasQuery && searched && hits.length === 0 && !loading}
				<p class="notice">
					Nothing matches <strong>“{searched}”</strong>. Try fewer words or a tag.
				</p>
			{:else if list.length > 0}
				{#if !hasQuery}
					<h2 class="section">Recently published</h2>
				{/if}
				<ul id="results" role="listbox" aria-label="Search results">
					{#each list as hit, i (hit.slug)}
						<li id={`hit-${i}`} role="option" aria-selected={i === active}>
							<a
								href={`/docs/${hit.slug}`}
								class="hit"
								class:active={i === active}
								onmouseenter={() => (active = i)}
							>
								<!-- eslint-disable svelte/no-at-html-tags -- title_html/snippet are escaped server-side; only <mark> survives (api/internal/search) -->
								<span class="hit-title">{@html hit.title_html || 'Untitled'}</span>
								{#if hasQuery && hit.snippet}
									<span class="hit-snippet">{@html hit.snippet}</span>
								{:else if hit.description}
									<span class="hit-snippet">{hit.description}</span>
								{/if}
								<!-- eslint-enable svelte/no-at-html-tags -->
								<span class="hit-meta">
									<span>{hit.author}</span>
									<span aria-hidden="true">·</span>
									<time datetime={hit.published_at}>{relativeTime(hit.published_at)}</time>
									{#each hit.tags.slice(0, 4) as tag (tag)}
										<span class="chip">{tag}</span>
									{/each}
								</span>
							</a>
						</li>
					{/each}
				</ul>
			{:else if !hasQuery}
				<p class="notice quiet">
					Nothing published yet. Press <strong>+</strong> to write the first entry.
				</p>
			{/if}
		</div>
	</div>
</div>

<style>
	.home {
		min-height: 100vh;
		min-height: 100dvh;
		display: flex;
		justify-content: center;
		padding: 0 1.25rem;
	}

	.column {
		width: 100%;
		max-width: 44rem;
		padding-top: max(18vh, 5rem);
		padding-bottom: 4rem;
		transition: padding-top 260ms var(--ease);
	}

	.searching .column {
		padding-top: 3.5rem;
	}

	.greeting {
		margin: 0 0 1.25rem;
		font-size: var(--text-xl);
		font-weight: 600;
		letter-spacing: -0.02em;
		text-align: center;
		transition:
			opacity 200ms var(--ease),
			margin 260ms var(--ease);
	}

	.searching .greeting {
		opacity: 0;
		height: 0;
		margin: 0;
		overflow: hidden;
	}

	.bar {
		display: flex;
		gap: 0.6rem;
		align-items: stretch;
	}

	.search {
		flex: 1;
		min-width: 0;
		display: flex;
		align-items: center;
		gap: 0.6rem;
		height: 3.5rem;
		padding: 0 1rem;
		border: 1px solid var(--border);
		border-radius: var(--radius-lg);
		background: var(--surface);
		box-shadow: var(--shadow-md);
		cursor: text;
		transition:
			border-color 120ms var(--ease),
			box-shadow 120ms var(--ease);
	}

	.search:focus-within {
		border-color: var(--accent);
		box-shadow:
			var(--shadow-md),
			0 0 0 3px var(--accent-soft);
	}

	.search-icon {
		display: grid;
		color: var(--fg-subtle);
	}

	.search input {
		flex: 1;
		min-width: 0;
		height: 100%;
		border: none;
		background: transparent;
		font-size: var(--text-md);
		outline: none;
	}

	.search input::placeholder {
		color: var(--fg-subtle);
	}

	.search input::-webkit-search-cancel-button {
		display: none;
	}

	.spinner {
		width: 1rem;
		height: 1rem;
		border: 2px solid var(--border-strong);
		border-top-color: var(--accent);
		border-radius: 50%;
		animation: spin 700ms linear infinite;
	}

	@keyframes spin {
		to {
			transform: rotate(360deg);
		}
	}

	.new {
		display: grid;
		place-items: center;
		width: 3.5rem;
		height: 3.5rem;
		padding: 0;
		border: 1px solid var(--accent);
		border-radius: var(--radius-lg);
		background: var(--accent);
		color: var(--accent-fg);
		box-shadow: var(--shadow-md);
		cursor: pointer;
		transition: background 120ms var(--ease);
	}

	.new:hover {
		background: var(--accent-hover);
	}

	.results {
		margin-top: 1.5rem;
	}

	.section {
		margin: 0 0 0.5rem;
		padding: 0 0.75rem;
		color: var(--fg-subtle);
		font-size: var(--text-xs);
		font-weight: 600;
		letter-spacing: 0.06em;
		text-transform: uppercase;
	}

	ul {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		gap: 2px;
	}

	.hit {
		display: grid;
		gap: 0.2rem;
		padding: 0.75rem;
		border-radius: var(--radius);
		color: var(--fg);
	}

	.hit:hover,
	.hit.active {
		background: var(--surface-hover);
		text-decoration: none;
	}

	.hit-title {
		font-size: var(--text-md);
		font-weight: 600;
		letter-spacing: -0.005em;
	}

	.hit-snippet {
		color: var(--fg-muted);
		font-size: var(--text-sm);
		line-height: 1.55;
		display: -webkit-box;
		-webkit-line-clamp: 2;
		line-clamp: 2;
		-webkit-box-orient: vertical;
		overflow: hidden;
	}

	.hit-meta {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 0.4rem;
		margin-top: 0.15rem;
		color: var(--fg-subtle);
		font-size: var(--text-xs);
	}

	.notice {
		margin: 0;
		padding: 0.75rem;
		color: var(--fg-muted);
		font-size: var(--text-sm);
	}

	.quiet {
		text-align: center;
	}

	@media (max-width: 600px) {
		.column {
			padding-top: 4.5rem;
		}

		.greeting {
			font-size: var(--text-lg);
		}
	}
</style>
