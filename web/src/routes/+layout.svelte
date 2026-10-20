<script lang="ts">
	import '@fontsource-variable/hanken-grotesk';
	import '@fontsource-variable/source-serif-4';
	import '@fontsource-variable/jetbrains-mono';
	import '../app.css';
	import type { Snippet } from 'svelte';

	let { children }: { children: Snippet } = $props();
</script>

{@render children()}
