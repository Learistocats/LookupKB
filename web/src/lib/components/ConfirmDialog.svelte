<!--
  ConfirmDialog — the generic "are you sure?" modal on a native <dialog>. The parent
  owns the action: it binds `open`, handles `onConfirm` (showing `busy` while it
  works) and `onCancel` (also fired by Esc / the dialog closing). `danger` styles
  the confirm button as destructive. Optional `children` render between the message
  and the buttons for richer prompts (the publish metadata form uses it).

  Presentational and generic on purpose: reused by draft delete (LOO-58), publish,
  and later prompts such as "make this tag protected?".
-->
<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		open = $bindable(false),
		title,
		message = '',
		confirmLabel = 'Confirm',
		cancelLabel = 'Cancel',
		danger = false,
		busy = false,
		ariaLabel,
		onConfirm,
		onCancel,
		children
	}: {
		open?: boolean;
		title: string;
		message?: string;
		confirmLabel?: string;
		cancelLabel?: string;
		danger?: boolean;
		busy?: boolean;
		ariaLabel?: string;
		onConfirm: () => void;
		onCancel: () => void;
		children?: Snippet;
	} = $props();

	let dialogEl = $state<HTMLDialogElement>();

	// Drive the native <dialog> from the `open` prop so the parent controls it.
	$effect(() => {
		if (!dialogEl) return;
		if (open && !dialogEl.open) dialogEl.showModal();
		else if (!open && dialogEl.open) dialogEl.close();
	});

	// Esc fires `cancel` then `close`; while busy, keep the dialog up so the result
	// of the in-flight action is not lost.
	function onDialogCancel(ev: Event) {
		if (busy) ev.preventDefault();
	}
</script>

<dialog
	bind:this={dialogEl}
	oncancel={onDialogCancel}
	onclose={onCancel}
	aria-label={ariaLabel ?? title}
>
	<header>
		<h2>{title}</h2>
		{#if message}
			<p class="hint">{message}</p>
		{/if}
	</header>

	{@render children?.()}

	<div class="actions">
		<button type="button" class="btn btn-ghost" onclick={onCancel} disabled={busy}>
			{cancelLabel}
		</button>
		<button
			type="button"
			class="btn"
			class:btn-primary={!danger}
			class:confirm-danger={danger}
			onclick={onConfirm}
			disabled={busy}
		>
			{busy ? 'Working…' : confirmLabel}
		</button>
	</div>
</dialog>

<style>
	dialog {
		width: min(34rem, calc(100vw - 2rem));
		max-height: calc(100dvh - 2rem);
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
		gap: 1rem;
	}

	dialog::backdrop {
		background: var(--scrim);
		backdrop-filter: blur(2px);
	}

	header {
		display: grid;
		gap: 0.25rem;
	}

	h2 {
		margin: 0;
		font-size: var(--text-lg);
		font-weight: 650;
		letter-spacing: -0.01em;
	}

	.hint {
		margin: 0;
		color: var(--fg-muted);
		font-size: var(--text-sm);
	}

	.actions {
		display: flex;
		justify-content: flex-end;
		gap: 0.5rem;
		margin-top: 0.25rem;
	}

	.confirm-danger {
		color: var(--danger);
		border-color: var(--danger);
		background: var(--danger-soft);
	}

	.confirm-danger:hover:not(:disabled) {
		color: var(--danger);
		border-color: var(--danger);
		background: var(--danger-soft);
		filter: brightness(0.96);
	}
</style>
