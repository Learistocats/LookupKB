<script lang="ts">
	import { onMount } from 'svelte';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();

	// Keep relative "updated" times live without a round-trip.
	let nowTick = $state(Date.now());
	onMount(() => {
		const tick = setInterval(() => (nowTick = Date.now()), 30_000);
		return () => clearInterval(tick);
	});

	function relativeTime(iso: string, now: number): string {
		const then = Date.parse(iso);
		if (Number.isNaN(then)) return '';
		const s = Math.max(0, Math.floor((now - then) / 1000));
		if (s < 60) return 'just now';
		const m = Math.floor(s / 60);
		if (m < 60) return `${m}m ago`;
		const h = Math.floor(m / 60);
		if (h < 24) return `${h}h ago`;
		const d = Math.floor(h / 24);
		return `${d}d ago`;
	}
</script>

<svelte:head>
	<title>Workspace · LookupKB</title>
</svelte:head>

<main>
	<a class="back" href="/">← Home</a>
	<header>
		<h1>Your drafts</h1>
		<form method="POST" action="?/createDraft">
			<button type="submit">New draft</button>
		</form>
	</header>

	{#if data.drafts.length === 0}
		<p class="empty">No drafts yet. Create one to start writing.</p>
	{:else}
		<ul class="drafts">
			{#each data.drafts as draft (draft.id)}
				<li>
					<a href="/drafts/{draft.id}">
						<span class="title" class:untitled={!draft.title}>{draft.title || 'Untitled'}</span>
						<span class="updated">{relativeTime(draft.updated_at, nowTick)}</span>
					</a>
				</li>
			{/each}
		</ul>
	{/if}
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

	header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 1rem;
		margin-top: 0.75rem;
	}

	h1 {
		margin: 0;
		font-size: 1.75rem;
		letter-spacing: -0.02em;
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

	button:hover {
		background: #23272e;
	}

	.empty {
		margin-top: 2rem;
		color: #9aa0a6;
	}

	.drafts {
		list-style: none;
		margin: 1.5rem 0 0;
		padding: 0;
		display: grid;
		gap: 0.5rem;
	}

	.drafts a {
		display: flex;
		align-items: baseline;
		justify-content: space-between;
		gap: 1rem;
		padding: 0.85rem 1rem;
		border: 1px solid #23262b;
		border-radius: 0.6rem;
		background: #131519;
		color: #e8eaed;
		text-decoration: none;
	}

	.drafts a:hover {
		background: #1a1d22;
		border-color: #2a2e35;
	}

	.title {
		font-weight: 600;
	}

	.title.untitled {
		color: #9aa0a6;
		font-weight: 500;
		font-style: italic;
	}

	.updated {
		color: #9aa0a6;
		font-size: 0.85rem;
		white-space: nowrap;
		font-variant-numeric: tabular-nums;
	}
</style>
