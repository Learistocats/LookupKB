<script lang="ts">
	// Shared frame for the login/signup pages: heading, tagline and form styling.
	import type { Snippet } from 'svelte';

	let { tagline, children }: { tagline: string; children: Snippet } = $props();
</script>

<main class="shell">
	<h1>LookupKB</h1>
	<p class="tagline">{tagline}</p>
	{@render children()}
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

	.shell {
		max-width: 24rem;
		margin: 0 auto;
		padding: 4rem 1.5rem;
	}

	h1 {
		margin: 0;
		font-size: 2.5rem;
		letter-spacing: -0.02em;
	}

	.tagline {
		color: #9aa0a6;
		margin-top: 0.25rem;
	}

	.shell :global(form) {
		display: flex;
		flex-direction: column;
		gap: 1rem;
		margin-top: 2rem;
		padding: 1.5rem;
		border: 1px solid #23262b;
		border-radius: 0.75rem;
		background: #131519;
	}

	.shell :global(label) {
		display: flex;
		flex-direction: column;
		gap: 0.35rem;
		font-size: 0.9rem;
		color: #9aa0a6;
	}

	.shell :global(label em) {
		font-style: normal;
		color: #6b7178;
	}

	.shell :global(input) {
		padding: 0.55rem 0.7rem;
		border: 1px solid #2a2e35;
		border-radius: 0.5rem;
		background: #0b0c0e;
		color: #e8eaed;
		font: inherit;
	}

	.shell :global(input:focus) {
		outline: 2px solid #93c5fd;
		outline-offset: 1px;
	}

	.shell :global(button) {
		padding: 0.6rem 1rem;
		border: 1px solid #2a2e35;
		border-radius: 0.5rem;
		background: #1a1d22;
		color: #e8eaed;
		cursor: pointer;
		font: inherit;
	}

	.shell :global(button:hover) {
		background: #23272e;
	}

	.shell :global(.error) {
		margin: 0;
		color: #f87171;
		font-size: 0.9rem;
	}

	.shell :global(.alt) {
		color: #9aa0a6;
		font-size: 0.9rem;
	}

	.shell :global(a) {
		color: #93c5fd;
	}
</style>
