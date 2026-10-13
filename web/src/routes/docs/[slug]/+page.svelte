<script lang="ts">
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();
	const doc = $derived(data.doc);
	const displayTitle = $derived(doc.title ?? 'Untitled');

	// Format the publish date unobtrusively (NFR-3). Falls back to the raw string if
	// it isn't a parseable timestamp.
	function formatDate(iso: string): string {
		const d = new Date(iso);
		if (Number.isNaN(d.getTime())) return iso;
		return d.toLocaleDateString(undefined, { year: 'numeric', month: 'long', day: 'numeric' });
	}
</script>

<svelte:head>
	<title>{displayTitle} · LookupKB</title>
</svelte:head>

<main>
	<a class="back" href="/">← Home</a>
	<h1>{displayTitle}</h1>
	{#if doc.description}
		<p class="description">{doc.description}</p>
	{/if}

	<!-- Minimal metadata (AC-8.5): author, published date, tags. -->
	<div class="meta">
		{#if doc.author_display_name}
			<span>{doc.author_display_name}</span>
		{/if}
		<span>{formatDate(doc.published_at)}</span>
	</div>
	{#if doc.tags.length > 0}
		<ul class="tags">
			{#each doc.tags as tag (tag)}
				<li class="chip">{tag}</li>
			{/each}
		</ul>
	{/if}

	<!--
		{@html} is SAFE here and ONLY here: `rendered_html` is produced exclusively by
		the server publish pipeline (goldmark → bluemonday strict allowlist, see
		api/internal/render), sanitized once at publish and cached. It is the server's
		own trusted output, not client/user input. Do NOT add a client-side sanitize
		pass (defeats the "render once, trust it" design), and do NOT reuse {@html} for
		any content that did not come through that pipeline.
	-->
	<!-- eslint-disable-next-line svelte/no-at-html-tags -- server-sanitized publish output; see the note above -->
	<article class="doc-content">{@html doc.rendered_html}</article>
</main>

<style>
	:global(body) {
		margin: 0;
		font-family:
			system-ui,
			-apple-system,
			'Segoe UI',
			Roboto,
			sans-serif;
		background: #0b0c0e;
		color: #e8eaed;
	}

	main {
		max-width: 48rem;
		margin: 0 auto;
		padding: 3rem 1.5rem;
	}

	.back {
		color: #93c5fd;
		text-decoration: none;
		font-size: 0.9rem;
	}

	h1 {
		margin: 0.75rem 0 0;
		font-size: 1.75rem;
		letter-spacing: -0.02em;
	}

	.description {
		color: #c3c7cc;
		margin: 0.5rem 0 0;
	}

	.meta {
		display: flex;
		flex-wrap: wrap;
		gap: 0.5rem 1rem;
		color: #9aa0a6;
		font-size: 0.9rem;
		margin-top: 0.75rem;
	}

	.tags {
		list-style: none;
		display: flex;
		flex-wrap: wrap;
		gap: 0.4rem;
		padding: 0;
		margin: 0.75rem 0 0;
	}

	.chip {
		display: inline-flex;
		align-items: center;
		padding: 0.2rem 0.5rem;
		border-radius: 999px;
		background: #1a1d22;
		border: 1px solid #2a2e35;
		color: #e8eaed;
		font-size: 0.85rem;
	}

	.doc-content {
		display: block;
		margin-top: 2rem;
		padding-top: 1.5rem;
		border-top: 1px solid #2a2e35;
		line-height: 1.65;
		overflow-wrap: break-word;
	}

	/* Styling for the server-rendered Markdown body. :global because the HTML is
	   injected at runtime, so Svelte's scoping can't see these elements. */
	.doc-content :global(h1),
	.doc-content :global(h2),
	.doc-content :global(h3),
	.doc-content :global(h4) {
		letter-spacing: -0.01em;
		line-height: 1.3;
		margin: 1.6rem 0 0.6rem;
	}

	.doc-content :global(a) {
		color: #93c5fd;
	}

	.doc-content :global(p),
	.doc-content :global(ul),
	.doc-content :global(ol),
	.doc-content :global(blockquote) {
		margin: 0.8rem 0;
	}

	.doc-content :global(code) {
		background: #131519;
		border: 1px solid #23262b;
		border-radius: 0.3rem;
		padding: 0.1rem 0.35rem;
		font-size: 0.9em;
	}

	.doc-content :global(pre) {
		background: #131519;
		border: 1px solid #23262b;
		border-radius: 0.5rem;
		padding: 0.9rem 1rem;
		overflow-x: auto;
	}

	.doc-content :global(pre code) {
		background: none;
		border: none;
		padding: 0;
	}

	.doc-content :global(blockquote) {
		border-left: 3px solid #2a2e35;
		padding-left: 1rem;
		color: #c3c7cc;
	}

	.doc-content :global(img) {
		max-width: 100%;
		height: auto;
	}

	.doc-content :global(table) {
		border-collapse: collapse;
		width: 100%;
		display: block;
		overflow-x: auto;
	}

	.doc-content :global(th),
	.doc-content :global(td) {
		border: 1px solid #2a2e35;
		padding: 0.4rem 0.6rem;
		text-align: left;
	}
</style>
