<script lang="ts">
	import { enhance } from '$app/forms';
	import type { ActionData, PageData } from './$types';

	let { data, form }: { data: PageData; form: ActionData } = $props();
</script>

<svelte:head><title>Groups · Admin · LookupKB</title></svelte:head>

<section class="card">
	<h2>New group</h2>
	<form method="POST" action="?/create" class="inline-form" use:enhance>
		<label class="visually-hidden" for="group-name">Group name</label>
		<input
			id="group-name"
			class="input"
			name="name"
			required
			maxlength="100"
			placeholder="e.g. security"
			autocomplete="off"
		/>
		<button class="btn btn-primary" type="submit">Create group</button>
	</form>
	{#if form?.message}<p class="error-text" role="alert">{form.message}</p>{/if}
</section>

<section class="card">
	<h2>Groups</h2>
	{#if data.groups.length === 0}
		<p class="empty">No groups yet.</p>
	{:else}
		<ul class="rows">
			{#each data.groups as g (g.id)}
				<li>
					<a class="grow" href={`/admin/groups/${g.id}`}>{g.name}</a>
					<span class="muted">
						{g.member_count ?? 0}
						{g.member_count === 1 ? 'member' : 'members'} · {g.tag_count ?? 0}
						{g.tag_count === 1 ? 'tag' : 'tags'}
					</span>
				</li>
			{/each}
		</ul>
	{/if}
</section>
