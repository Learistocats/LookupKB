<script lang="ts">
	import { invalidateAll } from '$app/navigation';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();

	let checking = $state(false);

	async function check() {
		checking = true;
		await invalidateAll();
		checking = false;
	}
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
				<dd>{data.user.email}</dd>
				<dt>groups</dt>
				<dd>{data.me?.groups.length ? data.me.groups.join(', ') : '(none)'}</dd>
			</dl>
			{#if data.me}
				<p class="badge ok">API verified token (/api/v1/me)</p>
			{:else if data.meError}
				<p class="badge error">API /me: {data.meError}</p>
			{/if}
			<div class="workspace-actions">
				<form method="POST" action="?/createDraft">
					<button type="submit">New draft</button>
				</form>
				<a class="workspace" href="/drafts">Your drafts →</a>
			</div>
			<form method="POST" action="/auth/logout">
				<button type="submit" class="logout">Log out</button>
			</form>
		</section>
	{/if}

	<section class="status" aria-live="polite">
		<h2>API health</h2>
		{#if checking}
			<p class="badge loading">Checking <code>/healthz</code>…</p>
		{:else if data.health.ok}
			<p class="badge ok">API is up</p>
			<dl>
				<dt>service</dt>
				<dd>{data.health.health.service}</dd>
				<dt>status</dt>
				<dd>{data.health.health.status}</dd>
				<dt>version</dt>
				<dd>{data.health.health.version}</dd>
			</dl>
		{:else}
			<p class="badge error">API unreachable</p>
			<p class="detail">{data.health.message}</p>
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

	.workspace-actions {
		display: flex;
		align-items: center;
		gap: 1.25rem;
		margin-top: 0.5rem;
	}

	.workspace-actions form button {
		margin-top: 0.75rem;
	}

	.workspace {
		color: #93c5fd;
		text-decoration: none;
		font-size: 0.9rem;
	}

	.logout {
		display: inline-block;
		margin-top: 1.25rem;
		color: #93c5fd;
	}
</style>
