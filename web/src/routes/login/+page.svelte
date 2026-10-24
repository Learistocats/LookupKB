<script lang="ts">
	import AuthShell from '$lib/components/AuthShell.svelte';
	import type { ActionData, PageData } from './$types';

	let { data, form }: { data: PageData; form: ActionData } = $props();
</script>

<svelte:head>
	<title>Sign in · LookupKB</title>
</svelte:head>

<AuthShell tagline="Sign in">
	<form class="stack" method="POST">
		<input type="hidden" name="returnTo" value={data.returnTo} />
		<label class="field">
			<span class="field-label">Email</span>
			<input
				class="input input-lg"
				name="email"
				type="email"
				autocomplete="email"
				required
				value={form?.email ?? ''}
			/>
		</label>
		<label class="field">
			<span class="field-label">Password</span>
			<input
				class="input input-lg"
				name="password"
				type="password"
				autocomplete="current-password"
				required
			/>
		</label>
		{#if form?.error}
			<p class="alert" role="alert">{form.error}</p>
		{/if}
		<button class="btn btn-primary btn-lg" type="submit">Sign in</button>
	</form>

	{#snippet footer()}No account yet? <a href="/signup">Create one</a>{/snippet}
</AuthShell>
