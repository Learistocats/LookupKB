<script lang="ts">
	import type { Snippet } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import Sidebar from '$lib/components/Sidebar.svelte';
	import type { LayoutData } from './$types';

	let { data, children }: { data: LayoutData; children: Snippet } = $props();

	// svelte-ignore state_referenced_locally
	let open = $state(data.panelOpen);
	// svelte-ignore state_referenced_locally
	let theme = $state(data.theme);

	// Ctrl/⌘+K opens search from anywhere in the signed-in app. Listening in the
	// capture phase lets it win over focused fields and the editor. Off the home
	// page it navigates there (the editor flushes pending edits before leaving);
	// the home page focuses its search box on mount.
	$effect(() => {
		const onKey = (e: KeyboardEvent) => {
			if (!(e.ctrlKey || e.metaKey) || e.shiftKey || e.altKey || e.key.toLowerCase() !== 'k')
				return;
			e.preventDefault();
			e.stopPropagation();
			if (page.url.pathname === '/') {
				const box = document.getElementById('q') as HTMLInputElement | null;
				box?.focus();
				box?.select();
			} else {
				void goto('/');
			}
		};
		window.addEventListener('keydown', onKey, true);
		return () => window.removeEventListener('keydown', onKey, true);
	});
</script>

<div class="shell" class:panel-open={open}>
	<Sidebar entries={data.entries} email={data.user?.email ?? null} bind:open bind:theme />
	<div class="main">
		<!-- Remount per entry: the editor seeds its state once, so moving from one
		     entry (or /entries/new) to another must start fresh. Keyed on the route
		     param, not the URL, so a new entry's shallow URL swap keeps the editor. -->
		{#key page.params.id}
			{@render children()}
		{/key}
	</div>
</div>

<style>
	.shell {
		min-height: 100%;
	}

	.main {
		min-height: 100vh;
		min-height: 100dvh;
		transition: padding-left 220ms var(--ease);
	}

	@media (min-width: 900px) {
		.panel-open .main {
			padding-left: var(--panel-width);
		}
	}
</style>
