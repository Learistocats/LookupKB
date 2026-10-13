<script lang="ts">
	import AuthShell from '$lib/components/AuthShell.svelte';
	import type { ActionData } from './$types';

	let { form }: { form: ActionData } = $props();
</script>

<svelte:head>
	<title>Create account · LookupKB</title>
</svelte:head>

<AuthShell tagline="Create your account.">
	<form method="POST">
		<label>
			<span>Email</span>
			<input name="email" type="email" autocomplete="email" required value={form?.email ?? ''} />
		</label>
		<label>
			<span>Display name <em>(optional)</em></span>
			<input
				name="display_name"
				type="text"
				autocomplete="name"
				maxlength="100"
				value={form?.displayName ?? ''}
			/>
		</label>
		<label>
			<span>Password <em>(at least 12 characters)</em></span>
			<input name="password" type="password" autocomplete="new-password" minlength="12" required />
		</label>
		<label>
			<span>Confirm password</span>
			<input name="confirm" type="password" autocomplete="new-password" minlength="12" required />
		</label>
		{#if form?.error}
			<p class="error" role="alert">{form.error}</p>
		{/if}
		<button type="submit">Create account</button>
	</form>

	<p class="alt">Already have an account? <a href="/login">Sign in</a></p>
</AuthShell>
