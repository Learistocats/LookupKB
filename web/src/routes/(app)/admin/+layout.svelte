<script lang="ts">
	import type { Snippet } from 'svelte';
	import { page } from '$app/state';

	let { children }: { children: Snippet } = $props();

	const tabs = [
		{ href: '/admin/groups', label: 'Groups' },
		{ href: '/admin/users', label: 'Users' }
	];
</script>

<div class="admin">
	<header>
		<h1>Admin</h1>
		<nav aria-label="Admin sections">
			{#each tabs as t (t.href)}
				<a
					href={t.href}
					class:active={page.url.pathname.startsWith(t.href)}
					aria-current={page.url.pathname.startsWith(t.href) ? 'page' : undefined}>{t.label}</a
				>
			{/each}
		</nav>
	</header>
	{@render children()}
</div>

<style>
	.admin {
		max-width: 48rem;
		margin: 0 auto;
		padding: 2.5rem 1rem 4rem;
		display: grid;
		gap: 1.5rem;
	}

	header {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		justify-content: space-between;
		gap: 0.75rem;
	}

	h1 {
		margin: 0;
		font-size: var(--text-xl);
		font-weight: 700;
		letter-spacing: -0.01em;
	}

	nav {
		display: flex;
		gap: 0.25rem;
	}

	nav a {
		padding: 0.35rem 0.75rem;
		border-radius: var(--radius-sm);
		color: var(--fg-muted);
		font-size: var(--text-sm);
	}

	nav a:hover {
		background: var(--surface-hover);
		color: var(--fg);
		text-decoration: none;
	}

	nav a.active {
		background: var(--accent-soft);
		color: var(--fg);
	}

	/* Shared admin building blocks for the child pages. */
	.admin :global(.card) {
		display: grid;
		gap: 0.75rem;
		padding: 1.25rem;
		border: 1px solid var(--border);
		border-radius: var(--radius-lg);
		background: var(--surface);
	}

	.admin :global(.card h2) {
		margin: 0;
		font-size: var(--text-md);
		font-weight: 650;
	}

	.admin :global(.inline-form) {
		display: flex;
		flex-wrap: wrap;
		gap: 0.5rem;
		align-items: center;
	}

	.admin :global(.inline-form .input) {
		flex: 1;
		min-width: 12rem;
	}

	.admin :global(.rows) {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
	}

	.admin :global(.rows > li) {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 0.5rem;
		padding: 0.5rem 0;
		border-top: 1px solid var(--border);
		font-size: var(--text-sm);
	}

	.admin :global(.rows > li:first-child) {
		border-top: none;
	}

	.admin :global(.grow) {
		flex: 1;
		min-width: 0;
		overflow-wrap: anywhere;
	}

	.admin :global(.muted) {
		color: var(--fg-subtle);
		font-size: var(--text-sm);
	}

	.admin :global(.empty) {
		margin: 0;
		color: var(--fg-subtle);
		font-size: var(--text-sm);
	}
</style>
