<script lang="ts">
	import Icon from '$lib/components/Icon.svelte';
	import { longDate } from '$lib/time';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();
	const doc = $derived(data.doc);
	const displayTitle = $derived(doc.title ?? 'Untitled');
</script>

<svelte:head>
	<title>{displayTitle} · LookupKB</title>
	{#if doc.description}<meta name="description" content={doc.description} />{/if}
</svelte:head>

<article class="reader">
	<header>
		<div class="top">
			<a class="back" href="/">
				<Icon name="search" size={15} />
				Search
			</a>
			{#if doc.id}
				<a class="btn btn-ghost edit" href={`/entries/${doc.id}`}>
					<Icon name="pen" size={15} /> Edit
				</a>
			{/if}
		</div>
		<h1>{displayTitle}</h1>
		{#if doc.description}
			<p class="lede">{doc.description}</p>
		{/if}
		<!-- Minimal metadata (AC-8.5): author, published date, tags. -->
		<div class="byline">
			{#if doc.author_display_name}
				<span>{doc.author_display_name}</span>
				<span aria-hidden="true">·</span>
			{/if}
			<time datetime={doc.published_at}>{longDate(doc.published_at)}</time>
			{#if doc.tags.length > 0}
				<ul class="tags" aria-label="Tags">
					{#each doc.tags as tag (tag)}
						<li class="chip">{tag}</li>
					{/each}
				</ul>
			{/if}
		</div>
	</header>

	<!--
		{@html} is SAFE here and ONLY here: `rendered_html` is produced exclusively by
		the server publish pipeline (goldmark → bluemonday strict allowlist, see
		api/internal/render), sanitized once at publish and cached. Do NOT add a
		client-side sanitize pass, and do NOT reuse {@html} for content that did not
		come through that pipeline.
	-->
	<!-- eslint-disable-next-line svelte/no-at-html-tags -- server-sanitized publish output; see the note above -->
	<div class="prose doc-content">{@html doc.rendered_html}</div>
</article>

<style>
	.reader {
		max-width: 42rem;
		margin: 0 auto;
		padding: 2rem 1.5rem 6rem;
	}

	.top {
		display: flex;
		align-items: center;
		justify-content: space-between;
		min-height: 2.25rem;
		margin-bottom: 2.5rem;
	}

	.back {
		display: inline-flex;
		align-items: center;
		gap: 0.4rem;
		color: var(--fg-muted);
		font-size: var(--text-sm);
	}

	.back:hover {
		color: var(--fg);
		text-decoration: none;
	}

	.edit {
		height: 2rem;
	}

	h1 {
		margin: 0;
		font-size: var(--text-2xl);
		font-weight: 700;
		letter-spacing: -0.025em;
		line-height: 1.15;
	}

	.lede {
		margin: 0.75rem 0 0;
		color: var(--fg-muted);
		font-family: var(--font-read);
		font-size: var(--text-lg);
		line-height: 1.5;
	}

	.byline {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 0.45rem;
		margin: 1.25rem 0 0;
		padding-bottom: 1.75rem;
		border-bottom: 1px solid var(--border);
		color: var(--fg-subtle);
		font-size: var(--text-sm);
	}

	.tags {
		display: flex;
		flex-wrap: wrap;
		gap: 0.3rem;
		margin: 0 0 0 0.4rem;
		padding: 0;
		list-style: none;
	}

	/* Published content: a serif reading column with sober UI-font headings. */
	.prose {
		margin-top: 2rem;
		font-family: var(--font-read);
		font-size: 1.125rem;
		line-height: 1.75;
		overflow-wrap: break-word;
	}

	.prose > :global(:first-child) {
		margin-top: 0;
	}

	.prose :global(:is(h1, h2, h3, h4, h5, h6)) {
		font-family: var(--font-ui);
		letter-spacing: -0.015em;
		line-height: 1.3;
		margin: 2.25rem 0 0.75rem;
	}

	.prose :global(h1) {
		font-size: 1.6rem;
	}

	.prose :global(h2) {
		font-size: 1.35rem;
	}

	.prose :global(h3) {
		font-size: 1.15rem;
	}

	.prose :global(p),
	.prose :global(ul),
	.prose :global(ol),
	.prose :global(blockquote),
	.prose :global(pre),
	.prose :global(table) {
		margin: 0 0 1.25rem;
	}

	.prose :global(li + li) {
		margin-top: 0.35rem;
	}

	.prose :global(a) {
		text-decoration: underline;
		text-decoration-color: color-mix(in srgb, var(--accent) 40%, transparent);
		text-underline-offset: 3px;
	}

	.prose :global(blockquote) {
		padding-left: 1rem;
		border-left: 3px solid var(--border-strong);
		color: var(--fg-muted);
	}

	.prose :global(code) {
		padding: 0.1em 0.35em;
		border-radius: 4px;
		background: var(--surface-2);
		font-family: var(--font-mono);
		font-size: 0.85em;
	}

	.prose :global(pre) {
		padding: 1rem 1.1rem;
		border: 1px solid var(--border);
		border-radius: var(--radius);
		background: var(--surface-2);
		overflow-x: auto;
		line-height: 1.55;
	}

	.prose :global(pre code) {
		padding: 0;
		background: none;
		font-size: 0.85rem;
	}

	.prose :global(table) {
		display: block;
		max-width: 100%;
		overflow-x: auto;
		border-collapse: collapse;
		font-family: var(--font-ui);
		font-size: var(--text-base);
	}

	.prose :global(th),
	.prose :global(td) {
		padding: 0.5rem 0.75rem;
		border-bottom: 1px solid var(--border);
		text-align: left;
	}

	.prose :global(th) {
		color: var(--fg-muted);
		font-weight: 600;
	}

	.prose :global(hr) {
		margin: 2.5rem 0;
		border: none;
		border-top: 1px solid var(--border);
	}

	.prose :global(img) {
		max-width: 100%;
		border-radius: var(--radius-sm);
	}

	/* GFM task lists: the checkbox replaces the bullet. */
	.prose :global(li:has(> input[type='checkbox'])) {
		list-style: none;
		margin-left: -1.35em;
	}

	.prose :global(input[type='checkbox']) {
		accent-color: var(--accent);
		margin-right: 0.4rem;
	}

	@media (max-width: 600px) {
		.reader {
			padding-top: 1rem;
		}

		h1 {
			font-size: var(--text-xl);
		}

		.prose {
			font-size: 1.0625rem;
		}
	}
</style>
