<script lang="ts">
	import type { Snippet } from 'svelte';
	import { page } from '$app/state';

	let { children }: { children: Snippet } = $props();

	const tabs = [
		{ href: '/admin/groups', label: 'Groups' },
		{ href: '/admin/users', label: 'Users' }
	];
</script>

<div class="admin page-column">
	<header>
		<h1 class="page-title">Admin</h1>
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
		--column: 48rem;
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

	/* Shared by the admin child pages. */
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

	.admin :global(.grow) {
		flex: 1;
		min-width: 0;
		overflow-wrap: anywhere;
	}
</style>
