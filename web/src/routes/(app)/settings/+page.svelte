<!--
  Settings → API tokens. Create a Personal Access Token (name + optional expiry),
  see its secret exactly once, list tokens by their non-secret prefix, and revoke
  with an inline two-step confirm. Works without JS (plain form posts).
-->
<script lang="ts">
	import { enhance } from '$app/forms';
	import { now, relativeTime, longDate } from '$lib/time';
	import {
		DEFAULT_EXPIRY,
		EXPIRY_OPTIONS,
		TOKEN_NAME_MAX,
		maskedToken,
		tokenStatus
	} from '$lib/tokens';
	import type { ActionData, PageData } from './$types';

	let { data, form }: { data: PageData; form: ActionData } = $props();

	let confirming = $state<string | null>(null);
	let copied = $state(false);
	let dismissed = $state(false);

	const created = $derived(form?.action === 'create' && !dismissed ? form.created : undefined);
	const createError = $derived(form?.action === 'create' ? form.error : undefined);
	const revokeError = $derived(form?.action === 'revoke' ? form.error : undefined);

	async function copy(token: string) {
		try {
			await navigator.clipboard.writeText(token);
			copied = true;
			setTimeout(() => (copied = false), 2000);
		} catch {
			copied = false;
		}
	}

	const statusLabel = { active: 'Active', expired: 'Expired', revoked: 'Revoked' } as const;
</script>

<svelte:head>
	<title>Settings · LookupKB</title>
</svelte:head>

<div class="settings page-column">
	<h1 class="page-title">Settings</h1>

	<section aria-labelledby="tokens-heading">
		<h2 id="tokens-heading">API tokens</h2>
		<p class="lede">
			Personal access tokens let scripts and tools read the knowledge base as you, with exactly your
			access. Send one as <code>Authorization: Bearer &lt;token&gt;</code>. Tokens are read-only:
			they cannot edit, publish or manage tokens.
		</p>

		{#if created}
			<div class="reveal" role="status">
				<p class="reveal-title">Token “{created.name}” created</p>
				<p class="reveal-note">Copy it now — you won’t be able to see it again.</p>
				<div class="reveal-row">
					<input
						class="input mono"
						readonly
						value={created.token}
						aria-label="New API token"
						onfocus={(e) => e.currentTarget.select()}
					/>
					<button class="btn btn-primary" type="button" onclick={() => copy(created.token)}>
						{copied ? 'Copied' : 'Copy'}
					</button>
				</div>
				<button class="btn btn-ghost done" type="button" onclick={() => (dismissed = true)}
					>Done</button
				>
			</div>
		{/if}

		<form
			class="create"
			method="POST"
			action="?/create"
			use:enhance={() =>
				async ({ update }) => {
					dismissed = false;
					copied = false;
					await update({ reset: true });
				}}
		>
			<label class="field name">
				<span class="field-label">Name</span>
				<input
					class="input"
					name="name"
					required
					maxlength={TOKEN_NAME_MAX}
					placeholder="e.g. CI pipeline"
					value={form?.action === 'create' && !form.created ? (form.name ?? '') : ''}
				/>
			</label>
			<label class="field">
				<span class="field-label">Expires</span>
				<select class="input" name="expires_in_days">
					{#each EXPIRY_OPTIONS as o (o.value)}
						<option value={o.value} selected={o.value === DEFAULT_EXPIRY}>{o.label}</option>
					{/each}
				</select>
			</label>
			<button class="btn btn-primary" type="submit">Create token</button>
		</form>
		{#if createError}
			<p class="error-text" role="alert">{createError}</p>
		{/if}

		{#if data.tokensError}
			<p class="error-text" role="alert">{data.tokensError}</p>
		{:else if data.tokens.length === 0}
			<p class="empty">You have no API tokens yet.</p>
		{:else}
			{#if revokeError}
				<p class="error-text" role="alert">{revokeError}</p>
			{/if}
			<ul class="rows tokens">
				{#each data.tokens as t (t.id)}
					{@const status = tokenStatus(t, now())}
					<li class:inactive={status !== 'active'}>
						<div class="meta">
							<div class="title-row">
								<span class="name">{t.name}</span>
								<span class="chip" class:chip-ok={status === 'active'}>{statusLabel[status]}</span>
							</div>
							<code class="prefix">{maskedToken(t.prefix)}</code>
							<dl>
								<div>
									<dt>Created</dt>
									<dd><time datetime={t.created_at}>{longDate(t.created_at)}</time></dd>
								</div>
								<div>
									<dt>Last used</dt>
									<dd>
										{#if t.last_used_at}
											<time datetime={t.last_used_at}>{relativeTime(t.last_used_at, now())}</time>
										{:else}
											Never
										{/if}
									</dd>
								</div>
								<div>
									<dt>Expires</dt>
									<dd>
										{#if t.expires_at}
											<time datetime={t.expires_at}>{longDate(t.expires_at)}</time>
										{:else}
											Never
										{/if}
									</dd>
								</div>
								{#if t.revoked_at}
									<div>
										<dt>Revoked</dt>
										<dd><time datetime={t.revoked_at}>{longDate(t.revoked_at)}</time></dd>
									</div>
								{/if}
							</dl>
						</div>
						{#if !t.revoked_at}
							<form
								class="revoke"
								method="POST"
								action="?/revoke"
								use:enhance={() =>
									async ({ update }) => {
										confirming = null;
										await update();
									}}
							>
								<input type="hidden" name="id" value={t.id} />
								{#if confirming === t.id}
									<button class="btn btn-ghost" type="button" onclick={() => (confirming = null)}
										>Cancel</button
									>
									<button class="btn btn-danger" type="submit">Confirm revoke</button>
								{:else}
									<button
										class="btn btn-danger"
										type="button"
										onclick={() => (confirming = t.id)}
										aria-label={`Revoke ${t.name}`}>Revoke</button
									>
								{/if}
							</form>
						{/if}
					</li>
				{/each}
			</ul>
		{/if}
	</section>
</div>

<style>
	.settings {
		padding: 2.5rem 1.5rem 6rem;
	}

	h1 {
		margin-bottom: 2rem;
	}

	h2 {
		margin: 0 0 0.35rem;
		font-size: var(--text-lg);
		font-weight: 650;
	}

	.lede {
		margin: 0 0 1.25rem;
		color: var(--fg-muted);
		font-size: var(--text-sm);
	}

	code,
	.mono {
		font-family: var(--font-mono);
		font-size: var(--text-sm);
	}

	.reveal {
		display: grid;
		gap: 0.5rem;
		margin-bottom: 1.25rem;
		padding: 1rem;
		border: 1px solid var(--ok);
		border-radius: var(--radius);
		background: var(--ok-soft);
	}

	.reveal-title {
		margin: 0;
		font-weight: 650;
	}

	.reveal-note {
		margin: 0;
		color: var(--fg-muted);
		font-size: var(--text-sm);
	}

	.reveal-row {
		display: flex;
		gap: 0.5rem;
	}

	.reveal-row .input {
		flex: 1;
		min-width: 0;
	}

	.done {
		justify-self: start;
	}

	.create {
		display: flex;
		flex-wrap: wrap;
		align-items: end;
		gap: 0.75rem;
		margin-bottom: 0.75rem;
	}

	.create .name {
		flex: 1 1 14rem;
	}

	.create select {
		min-width: 9rem;
	}

	.empty {
		margin-top: 1.5rem;
	}

	.tokens {
		margin-top: 1.5rem;
		border: 1px solid var(--border);
		border-radius: var(--radius);
		background: var(--surface);
	}

	.tokens li {
		align-items: flex-start;
		justify-content: space-between;
		gap: 0.75rem;
		padding: 0.9rem 1rem;
		font-size: var(--text-base);
	}

	.tokens li.inactive .name,
	.tokens li.inactive .prefix {
		color: var(--fg-subtle);
	}

	.meta {
		display: grid;
		gap: 0.3rem;
		min-width: 0;
	}

	.title-row {
		display: flex;
		align-items: center;
		gap: 0.5rem;
	}

	.name {
		font-weight: 600;
		overflow-wrap: anywhere;
	}

	.prefix {
		color: var(--fg-muted);
	}

	dl {
		display: flex;
		flex-wrap: wrap;
		gap: 0.25rem 1.25rem;
		margin: 0.2rem 0 0;
		font-size: var(--text-xs);
	}

	dl div {
		display: flex;
		gap: 0.35rem;
	}

	dt {
		color: var(--fg-subtle);
	}

	dd {
		margin: 0;
		color: var(--fg-muted);
		font-variant-numeric: tabular-nums;
	}

	.revoke {
		display: flex;
		gap: 0.4rem;
	}
</style>
