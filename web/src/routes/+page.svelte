<script lang="ts">
	import { onMount } from 'svelte';
	import { getHealth, type Health } from '$lib/api';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();

	type State =
		{ kind: 'loading' } | { kind: 'ok'; health: Health } | { kind: 'error'; message: string };

	let state = $state<State>({ kind: 'loading' });

	async function check() {
		state = { kind: 'loading' };
		try {
			const health = await getHealth();
			state = { kind: 'ok', health };
		} catch (err) {
			state = { kind: 'error', message: err instanceof Error ? err.message : String(err) };
		}
	}

	onMount(check);
</script>

<svelte:head>
	<title>LookupKB</title>
</svelte:head>

<main>
	<h1>LookupKB</h1>
	<p class="tagline">A fast, minimal knowledge base. Search-first, no folders.</p>

	{#if data.user}
		<section class="status">
			<h2>Signed in</h2>
			<dl>
				<dt>email</dt>
				<dd>{data.user.email || '—'}</dd>
				<dt>subject</dt>
				<dd>{data.user.sub}</dd>
				<dt>groups</dt>
				<dd>{data.user.groups.length ? data.user.groups.join(', ') : '(none)'}</dd>
			</dl>
			{#if data.me}
				<p class="badge ok">API verified token (/api/v1/me)</p>
			{:else if data.meError}
				<p class="badge error">API /me: {data.meError}</p>
			{/if}
			<a class="logout" href="/auth/logout">Log out</a>
		</section>
	{/if}

	<section class="status" aria-live="polite">
		<h2>API health</h2>
		{#if state.kind === 'loading'}
			<p class="badge loading">Checking <code>/healthz</code>…</p>
		{:else if state.kind === 'ok'}
			<p class="badge ok">API is up</p>
			<dl>
				<dt>service</dt>
				<dd>{state.health.service}</dd>
				<dt>status</dt>
				<dd>{state.health.status}</dd>
				<dt>version</dt>
				<dd>{state.health.version}</dd>
			</dl>
		{:else}
			<p class="badge error">API unreachable</p>
			<p class="detail">{state.message}</p>
		{/if}
		<button onclick={check}>Re-check</button>
	</section>
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
		max-width: 40rem;
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

	.status {
		margin-top: 2.5rem;
		padding: 1.5rem;
		border: 1px solid #23262b;
		border-radius: 0.75rem;
		background: #131519;
	}

	h2 {
		margin-top: 0;
		font-size: 1rem;
		text-transform: uppercase;
		letter-spacing: 0.08em;
		color: #9aa0a6;
	}

	.badge {
		display: inline-block;
		padding: 0.25rem 0.6rem;
		border-radius: 999px;
		font-weight: 600;
		font-size: 0.9rem;
	}

	.badge.ok {
		background: #0f2e1a;
		color: #4ade80;
	}

	.badge.error {
		background: #2e1113;
		color: #f87171;
	}

	.badge.loading {
		background: #1c2333;
		color: #93c5fd;
	}

	dl {
		display: grid;
		grid-template-columns: auto 1fr;
		gap: 0.25rem 1rem;
		margin: 1rem 0 0;
	}

	dt {
		color: #9aa0a6;
	}

	dd {
		margin: 0;
		font-variant-numeric: tabular-nums;
	}

	.detail {
		color: #f87171;
		font-family: ui-monospace, monospace;
		font-size: 0.85rem;
	}

	button {
		margin-top: 1.25rem;
		padding: 0.5rem 1rem;
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

	.logout {
		display: inline-block;
		margin-top: 1.25rem;
		color: #93c5fd;
	}
</style>
