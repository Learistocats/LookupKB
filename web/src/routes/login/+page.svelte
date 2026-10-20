<script lang="ts">
	import AuthShell from '$lib/components/AuthShell.svelte';
	import type { ActionData, PageData } from './$types';

	let { data, form }: { data: PageData; form: ActionData } = $props();
</script>

<svelte:head>
	<title>Sign in · LookupKB</title>
</svelte:head>

<AuthShell tagline="Sign in">
	<form method="POST">
		<input type="hidden" name="returnTo" value={data.returnTo} />
		<label>
			<span>Email</span>
			<input name="email" type="email" autocomplete="email" required value={form?.email ?? ''} />
		</label>
		<label>
			<span>Password</span>
			<input name="password" type="password" autocomplete="current-password" required />
		</label>
		{#if form?.error}
			<p class="error" role="alert">{form.error}</p>
		{/if}
		<button type="submit">Sign in</button>
	</form>

	<p class="alt">No account yet? <a href="/signup">Create one</a></p>
</AuthShell>
