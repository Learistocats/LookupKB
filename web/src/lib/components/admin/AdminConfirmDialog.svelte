<!--
  AdminConfirmDialog — a yes/no confirmation for consequential admin actions
  (protecting/unprotecting a tag, deleting a group). Native <dialog>, driven by the
  `open` prop like PublishDialog. Presentational: the page decides what happens on
  confirm. (A generic ConfirmDialog is landing separately; dedupe later.)
-->
<script lang="ts">
	let {
		open = $bindable(false),
		title = 'Are you sure?',
		message,
		confirmLabel = 'Confirm',
		danger = false,
		onConfirm,
		onCancel
	}: {
		open?: boolean;
		title?: string;
		message: string;
		confirmLabel?: string;
		danger?: boolean;
		onConfirm: () => void;
		onCancel: () => void;
	} = $props();

	let dialogEl = $state<HTMLDialogElement>();
	// Set when the dialog closes via Confirm, so the native close event (fired by
	// both buttons and Escape) only reports a cancel when it really was one.
	let confirmed = false;

	$effect(() => {
		if (!dialogEl) return;
		if (open && !dialogEl.open) {
			confirmed = false;
			dialogEl.showModal();
		} else if (!open && dialogEl.open) dialogEl.close();
	});

	function confirm() {
		confirmed = true;
		open = false;
		onConfirm();
	}

	function onClose() {
		if (confirmed) return;
		open = false;
		onCancel();
	}
</script>

<dialog bind:this={dialogEl} onclose={onClose} aria-labelledby="admin-confirm-title">
	<h2 id="admin-confirm-title">{title}</h2>
	<p class="message">{message}</p>
	<div class="actions">
		<button type="button" class="btn btn-ghost" onclick={() => dialogEl?.close()}>Cancel</button>
		<button
			type="button"
			class="btn"
			class:btn-primary={!danger}
			class:btn-danger={danger}
			onclick={confirm}
		>
			{confirmLabel}
		</button>
	</div>
</dialog>

<style>
	dialog {
		width: min(28rem, calc(100vw - 2rem));
		margin: auto;
		padding: 1.5rem;
		border: 1px solid var(--border);
		border-radius: var(--radius-lg);
		background: var(--surface);
		color: var(--fg);
		box-shadow: var(--shadow-md);
	}

	dialog[open] {
		display: grid;
		gap: 0.75rem;
	}

	dialog::backdrop {
		background: var(--scrim);
		backdrop-filter: blur(2px);
	}

	h2 {
		margin: 0;
		font-size: var(--text-lg);
		font-weight: 650;
		letter-spacing: -0.01em;
	}

	.message {
		margin: 0;
		color: var(--fg-muted);
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}

	.actions {
		display: flex;
		justify-content: flex-end;
		gap: 0.5rem;
		margin-top: 0.25rem;
	}
</style>
