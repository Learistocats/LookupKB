<!--
  TagInput — reusable tag entry with debounced autocomplete + inline validation
  (F6, AC-6.1/6.3). The repo's first shared component. It is intentionally NOT
  wired into a page yet: the publish confirm flow that hosts it is LOO-14. Bind
  `tags` to collect the selected slugs.

  All tag logic (normalize/validate/search/debounce) lives in $lib/tags so it is
  unit-tested independently and this component stays presentational.
-->
<script lang="ts">
	import {
		normalizeTag,
		validateTags,
		searchTags,
		debounce,
		defaultTagLimits,
		type TagLimits,
		type TagSuggestion
	} from '$lib/tags';

	let {
		tags = $bindable([]),
		limits = defaultTagLimits,
		label = 'Tags'
	}: { tags?: string[]; limits?: TagLimits; label?: string } = $props();

	let draft = $state('');
	let suggestions = $state<TagSuggestion[]>([]);

	// Live validation of the current set drives the inline error feedback.
	let errors = $derived(validateTags(tags, limits).errors);

	const runSearch = debounce(async (q: string) => {
		if (!q.trim()) {
			suggestions = [];
			return;
		}
		try {
			const hits = await searchTags(q);
			// Hide tags already chosen so the dropdown only offers new ones.
			suggestions = hits.filter((h) => !tags.includes(h.slug));
		} catch {
			suggestions = []; // suggestions are best-effort; typing still works.
		}
	}, 200);

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
	<label for="tag-entry">{label}</label>

	{#if tags.length > 0}
		<ul class="chips">
			{#each tags as tag (tag)}
				<li class="chip">
					<span>{tag}</span>
					<button type="button" aria-label={`Remove ${tag}`} onclick={() => removeTag(tag)}>
						×
					</button>
				</li>
			{/each}
		</ul>
	{/if}

	<input
		id="tag-entry"
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
						<span class="slug">{s.slug}</span>
						<span class="count">{s.usage_count}</span>
					</button>
				</li>
			{/each}
		</ul>
	{/if}

	{#each errors as err (err)}
		<span class="badge error">{err}</span>
	{/each}
</div>

<style>
	.tag-input {
		display: grid;
		gap: 0.5rem;
		position: relative;
	}

	label {
		color: #9aa0a6;
		font-size: 0.85rem;
		text-transform: uppercase;
		letter-spacing: 0.06em;
	}

	.chips {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-wrap: wrap;
		gap: 0.4rem;
	}

	.chip {
		display: inline-flex;
		align-items: center;
		gap: 0.35rem;
		padding: 0.2rem 0.5rem;
		border-radius: 999px;
		background: #1a1d22;
		border: 1px solid #2a2e35;
		color: #e8eaed;
		font-size: 0.85rem;
	}

	.chip button {
		border: none;
		background: none;
		color: #9aa0a6;
		cursor: pointer;
		font-size: 1rem;
		line-height: 1;
		padding: 0;
	}

	.chip button:hover {
		color: #f87171;
	}

	input {
		width: 100%;
		box-sizing: border-box;
		padding: 0.6rem 0.75rem;
		border: 1px solid #2a2e35;
		border-radius: 0.5rem;
		background: #131519;
		color: #e8eaed;
		font: inherit;
	}

	.suggestions {
		list-style: none;
		margin: 0;
		padding: 0.25rem;
		border: 1px solid #2a2e35;
		border-radius: 0.5rem;
		background: #131519;
		display: grid;
		gap: 0.15rem;
	}

	.suggestions button {
		width: 100%;
		display: flex;
		justify-content: space-between;
		align-items: center;
		padding: 0.4rem 0.5rem;
		border: none;
		border-radius: 0.35rem;
		background: none;
		color: #e8eaed;
		cursor: pointer;
		font: inherit;
	}

	.suggestions button:hover {
		background: #1a1d22;
	}

	.count {
		color: #9aa0a6;
		font-size: 0.8rem;
	}

	.badge {
		display: inline-block;
		padding: 0.25rem 0.6rem;
		border-radius: 999px;
		font-weight: 600;
		font-size: 0.85rem;
	}

	.badge.error {
		background: #2e1113;
		color: #f87171;
	}
</style>
