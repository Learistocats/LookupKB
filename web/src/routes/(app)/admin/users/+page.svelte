<script lang="ts">
	import { enhance } from '$app/forms';
	import type { ActionData, PageData } from './$types';

	let { data, form }: { data: PageData; form: ActionData } = $props();
</script>

<svelte:head><title>Users · Admin · LookupKB</title></svelte:head>

<section class="card">
	<h2>Users</h2>
	<p class="muted">Super-admins manage groups, tag policies and other super-admins.</p>
	{#if form?.message}<p class="error-text" role="alert">{form.message}</p>{/if}
	{#if data.users.length === 0}
		<p class="empty">No accounts yet.</p>
	{:else}
		<ul class="rows">
			{#each data.users as u (u.id)}
				<li>
					<span class="grow">
						{u.email}
						{#if u.display_name}<span class="muted">· {u.display_name}</span>{/if}
					</span>
					{#if u.is_super_admin}<span class="chip chip-ok">Super-admin</span>{/if}
					<form method="POST" action="?/setSuperAdmin" use:enhance>
						<input type="hidden" name="user_id" value={u.id} />
						<input type="hidden" name="value" value={u.is_super_admin ? 'false' : 'true'} />
						<button class="btn" class:btn-ghost={u.is_super_admin} type="submit">
							{u.is_super_admin ? 'Revoke super-admin' : 'Make super-admin'}
						</button>
					</form>
				</li>
			{/each}
		</ul>
	{/if}
</section>
