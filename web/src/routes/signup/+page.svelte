<script lang="ts">
	import AuthShell from '$lib/components/AuthShell.svelte';
	import type { ActionData } from './$types';

	let { form }: { form: ActionData } = $props();
</script>

<svelte:head>
	<title>Create account · LookupKB</title>
</svelte:head>

<AuthShell tagline="Create your account">
	<form class="stack" method="POST">
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
			<span class="field-label">Display name <em>(optional)</em></span>
			<input
				class="input input-lg"
				name="display_name"
				type="text"
				autocomplete="name"
				maxlength="100"
				value={form?.displayName ?? ''}
			/>
		</label>
		<label class="field">
			<span class="field-label">Password <em>(at least 12 characters)</em></span>
			<input
				class="input input-lg"
				name="password"
				type="password"
				autocomplete="new-password"
				minlength="12"
				required
			/>
		</label>
		<label class="field">
			<span class="field-label">Confirm password</span>
			<input
				class="input input-lg"
				name="confirm"
				type="password"
				autocomplete="new-password"
				minlength="12"
				required
			/>
		</label>
		{#if form?.error}
			<p class="alert" role="alert">{form.error}</p>
		{/if}
		<button class="btn btn-primary btn-lg" type="submit">Create account</button>
	</form>

	{#snippet footer()}Already have an account? <a href="/login">Sign in</a>{/snippet}
</AuthShell>
