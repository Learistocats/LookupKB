<script lang="ts">
	// Shared frame for the login/signup pages: wordmark, tagline and form styling.
	import type { Snippet } from 'svelte';

	let { tagline, children }: { tagline: string; children: Snippet } = $props();
</script>

<main class="shell">
	<div class="card">
		<p class="wordmark">LookupKB</p>
		<h1>{tagline}</h1>
		{@render children()}
	</div>
</main>

<style>
	.shell {
		min-height: 100vh;
		min-height: 100dvh;
		display: grid;
		place-items: center;
		padding: 2rem 1rem;
	}

	.card {
		width: 100%;
		max-width: 23rem;
	}

	.wordmark {
		margin: 0 0 2rem;
		font-weight: 700;
		font-size: var(--text-md);
		letter-spacing: -0.01em;
	}

	h1 {
		margin: 0 0 1.5rem;
		font-size: var(--text-xl);
		font-weight: 650;
		letter-spacing: -0.02em;
		line-height: 1.2;
	}

	.shell :global(form) {
		display: grid;
		gap: 1rem;
	}

	.shell :global(label) {
		display: grid;
		gap: 0.35rem;
		color: var(--fg-muted);
		font-size: var(--text-sm);
		font-weight: 550;
	}

	.shell :global(label em) {
		font-style: normal;
		font-weight: 400;
		color: var(--fg-subtle);
	}

	.shell :global(input) {
		height: 2.75rem;
		padding: 0 0.85rem;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: var(--surface);
		color: var(--fg);
		font-size: var(--text-base);
		transition: border-color 120ms var(--ease);
	}

	.shell :global(input:focus) {
		outline: none;
		border-color: var(--accent);
		box-shadow: 0 0 0 3px var(--accent-soft);
	}

	.shell :global(button[type='submit']) {
		height: 2.75rem;
		margin-top: 0.25rem;
		border: 1px solid var(--accent);
		border-radius: var(--radius-sm);
		background: var(--accent);
		color: var(--accent-fg);
		font-weight: 600;
		cursor: pointer;
		transition: background 120ms var(--ease);
	}

	.shell :global(button[type='submit']:hover) {
		background: var(--accent-hover);
	}

	.shell :global(.error) {
		margin: 0;
		padding: 0.6rem 0.75rem;
		border-radius: var(--radius-sm);
		background: var(--danger-soft);
		color: var(--danger);
		font-size: var(--text-sm);
	}

	.shell :global(.alt) {
		margin: 1.5rem 0 0;
		color: var(--fg-muted);
		font-size: var(--text-sm);
	}
</style>
