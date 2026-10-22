<!--
  Confirm-and-delete for one entry. Refreshes the history panel on success, then
  calls `onDeleted` so the host can react (e.g. leave the deleted entry's page).
-->
<script lang="ts">
	import { invalidate } from '$app/navigation';
	import ConfirmDialog from './ConfirmDialog.svelte';
	import { deleteEntry, DELETE_CONFIRM_MESSAGE } from '$lib/drafts';

	let {
		open = $bindable(false),
		id,
		onDeleted
	}: {
		open?: boolean;
		id: string | null | undefined;
		onDeleted?: () => void | Promise<void>;
	} = $props();

	let busy = $state(false);
	let error = $state('');

	$effect(() => {
		if (open) error = '';
	});

	async function confirm() {
		if (!id) return;
		busy = true;
		error = '';
		try {
			await deleteEntry(id);
			open = false;
			void invalidate('app:entries');
			await onDeleted?.();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Deleting failed. Try again.';
		} finally {
			busy = false;
		}
	}
</script>

<ConfirmDialog
	bind:open
	title="Delete draft"
	message={DELETE_CONFIRM_MESSAGE}
	confirmLabel="Delete"
	danger
	{busy}
	onConfirm={confirm}
	onCancel={() => (open = false)}
>
	{#if error}
		<p class="error-text">{error}</p>
	{/if}
</ConfirmDialog>
