<script lang="ts">
	// Shared frame for the login/signup pages: wordmark, tagline, form and footer link.
	import type { Snippet } from 'svelte';

	let { tagline, children, footer }: { tagline: string; children: Snippet; footer: Snippet } =
		$props();
</script>

<main class="shell">
	<div class="auth">
		<p class="wordmark">LookupKB</p>
		<h1>{tagline}</h1>
		{@render children()}
		<p class="alt">{@render footer()}</p>
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

	.auth {
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

	.alt {
		margin: 1.5rem 0 0;
		color: var(--fg-muted);
		font-size: var(--text-sm);
	}
</style>
