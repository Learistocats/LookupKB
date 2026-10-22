<!--
  TagInput — tag entry with debounced autocomplete and inline validation. Bind
  `tags` to collect the selected slugs. `protectedTags` (read-only for the parent)
  reports which selected tags are protected, learned from suggestions and, for
  prefilled or typed tags, resolved through the autocomplete endpoint.
-->
<script lang="ts">
	import Icon from './Icon.svelte';
	import { tagSuggester } from './tagSuggest';
	import { normalizeTag, validateTags, resolveProtected, type TagSuggestion } from '$lib/tags';

	let {
		tags = $bindable([]),
		protectedTags = $bindable([])
	}: {
		tags?: string[];
		protectedTags?: string[];
	} = $props();

	let draft = $state('');
	let suggestions = $state<TagSuggestion[]>([]);

	// slug → protected, learned from suggestions and resolved for other tags.
	let known = $state<Record<string, boolean>>({});

	$effect(() => {
		const missing = tags.filter((t) => !(t in known));
		if (missing.length === 0) return;
		let cancelled = false;
		resolveProtected(missing)
			.then((found) => {
				if (!cancelled) known = { ...known, ...found };
			})
			.catch(() => {
				// Best-effort hint only; the publish gate is authoritative.
			});
		return () => {
			cancelled = true;
		};
	});

	$effect(() => {
		protectedTags = tags.filter((t) => known[t]);
	});

	// Live validation of the current set drives the inline error feedback.
	let errors = $derived(validateTags(tags).errors);

	const runSearch = tagSuggester({
		exclude: (slug) => tags.includes(slug),
		onResult: (shown, all) => {
			known = { ...known, ...Object.fromEntries(all.map((h) => [h.slug, h.protected])) };
			suggestions = shown;
		}
	});

	function onInput(value: string) {
		draft = value;
		runSearch(value);
	}

	function addTag(raw: string) {
		const slug = normalizeTag(raw);
		if (slug && !tags.includes(slug)) {
			tags = [...tags, slug];
		}
		draft = '';
		suggestions = [];
	}

	function removeTag(slug: string) {
		tags = tags.filter((t) => t !== slug);
	}

	function onKeydown(e: KeyboardEvent) {
		if (e.key === 'Enter' || e.key === ',') {
			e.preventDefault();
			if (draft.trim()) addTag(draft);
		} else if (e.key === 'Backspace' && draft === '' && tags.length > 0) {
			removeTag(tags[tags.length - 1]);
		}
	}
</script>

<div class="tag-input">
	<label class="field-label" for="tag-entry">Tags</label>

	{#if tags.length > 0}
		<ul class="chips">
			{#each tags as tag (tag)}
				<li class="chip tag" class:chip-protected={known[tag]}>
					<span>{tag}</span>
					{#if known[tag]}<span class="visually-hidden">(protected)</span>{/if}
					<button type="button" aria-label={`Remove ${tag}`} onclick={() => removeTag(tag)}>
						<Icon name="x" size={12} />
					</button>
				</li>
			{/each}
		</ul>
	{/if}

	<input
		id="tag-entry"
		class="input"
		type="text"
		autocomplete="off"
		placeholder="Add a tag…"
		value={draft}
		oninput={(e) => onInput(e.currentTarget.value)}
		onkeydown={onKeydown}
	/>

	{#if suggestions.length > 0}
		<ul class="suggestions">
			{#each suggestions as s (s.slug)}
				<li>
					<button type="button" onclick={() => addTag(s.slug)}>
						<span class="slug">
							{s.slug}
							{#if s.protected}<span class="chip chip-protected">Protected</span>{/if}
						</span>
						<span class="count">{s.usage_count}</span>
					</button>
				</li>
			{/each}
		</ul>
	{/if}

	{#each errors as err (err)}
		<p class="error-text">{err}</p>
	{/each}
</div>

<style>
	.tag-input {
		display: grid;
		gap: 0.4rem;
		position: relative;
	}

	.chips {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-wrap: wrap;
		gap: 0.35rem;
	}

	.tag {
		height: 1.625rem;
		padding-right: 0.25rem;
		background: var(--accent-soft);
		color: var(--accent);
		font-size: var(--text-sm);
	}

	.tag button {
		display: grid;
		place-items: center;
		width: 1.125rem;
		height: 1.125rem;
		padding: 0;
		border: none;
		border-radius: 50%;
		background: none;
		color: inherit;
		cursor: pointer;
	}

	.tag button:hover {
		background: var(--surface);
	}

	.tag.chip-protected {
		background: var(--protected-soft);
		color: var(--protected);
	}

	.slug {
		display: inline-flex;
		align-items: center;
		gap: 0.4rem;
	}

	.suggestions {
		list-style: none;
		margin: 0;
		padding: 0.25rem;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: var(--surface);
		box-shadow: var(--shadow-md);
		display: grid;
		gap: 1px;
	}

	.suggestions button {
		width: 100%;
		display: flex;
		justify-content: space-between;
		align-items: center;
		padding: 0.4rem 0.5rem;
		border: none;
		border-radius: 4px;
		background: none;
		cursor: pointer;
		text-align: left;
	}

	.suggestions button:hover {
		background: var(--surface-hover);
	}

	.count {
		color: var(--fg-subtle);
		font-size: var(--text-xs);
		font-variant-numeric: tabular-nums;
	}
</style>
