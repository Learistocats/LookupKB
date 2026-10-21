<!--
  Icon — the handful of line icons the UI uses, inlined (no icon dependency).
  Decorative by default; pass `label` when the icon is the only content.
-->
<script lang="ts">
	export type IconName =
		| 'plus'
		| 'search'
		| 'chevron-right'
		| 'chevron-left'
		| 'sun'
		| 'moon'
		| 'monitor'
		| 'log-out'
		| 'x'
		| 'pen'
		| 'eye'
		| 'settings'
		| 'trash';

	let { name, size = 18, label }: { name: IconName; size?: number; label?: string } = $props();
</script>

<svg
	width={size}
	height={size}
	viewBox="0 0 24 24"
	fill="none"
	stroke="currentColor"
	stroke-width="1.75"
	stroke-linecap="round"
	stroke-linejoin="round"
	role={label ? 'img' : undefined}
	aria-label={label}
	aria-hidden={label ? undefined : 'true'}
>
	{#if name === 'plus'}
		<path d="M12 5v14M5 12h14" />
	{:else if name === 'search'}
		<circle cx="11" cy="11" r="6.5" /><path d="m20 20-4.2-4.2" />
	{:else if name === 'chevron-right'}
		<path d="m9 6 6 6-6 6" />
	{:else if name === 'chevron-left'}
		<path d="m15 6-6 6 6 6" />
	{:else if name === 'sun'}
		<circle cx="12" cy="12" r="4" /><path
			d="M12 2.5v2M12 19.5v2M4.6 4.6l1.4 1.4M18 18l1.4 1.4M2.5 12h2M19.5 12h2M4.6 19.4 6 18M18 6l1.4-1.4"
		/>
	{:else if name === 'moon'}
		<path d="M20 14.5A8 8 0 0 1 9.5 4a8 8 0 1 0 10.5 10.5Z" />
	{:else if name === 'monitor'}
		<rect x="3" y="4" width="18" height="12" rx="2" /><path d="M8 20h8M12 16v4" />
	{:else if name === 'log-out'}
		<path d="M15 4h3a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-3M10 17l5-5-5-5M15 12H4" />
	{:else if name === 'x'}
		<path d="M6 6l12 12M18 6 6 18" />
	{:else if name === 'pen'}
		<path d="M4 20h4L19 9a2.8 2.8 0 0 0-4-4L4 16v4Z" /><path d="m13.5 6.5 4 4" />
	{:else if name === 'trash'}
		<path d="M4 7h16M10 11v6M14 11v6M6 7l1 12a2 2 0 0 0 2 2h6a2 2 0 0 0 2-2l1-12M9 7V4h6v3" />
	{:else if name === 'eye'}
		<path d="M2.5 12S6 5 12 5s9.5 7 9.5 7-3.5 7-9.5 7-9.5-7-9.5-7Z" /><circle
			cx="12"
			cy="12"
			r="2.75"
		/>
	{:else if name === 'settings'}
		<circle cx="12" cy="12" r="3" /><path
			d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1Z"
		/>
	{/if}
</svg>
