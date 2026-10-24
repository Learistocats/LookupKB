import { debounce, searchTags, type TagSuggestion } from '$lib/tags';

/**
 * tagSuggester returns a debounced typeahead. `onResult` gets the suggestions to
 * show (minus `exclude`d slugs) and every hit returned; a blank or failed query
 * yields empty lists, since suggestions are best-effort.
 */
export function tagSuggester(opts: {
	exclude: (slug: string) => boolean;
	onResult: (shown: TagSuggestion[], all: TagSuggestion[]) => void;
}) {
	return debounce(async (q: string) => {
		if (!q.trim()) return opts.onResult([], []);
		try {
			const hits = await searchTags(q);
			opts.onResult(
				hits.filter((h) => !opts.exclude(h.slug)),
				hits
			);
		} catch {
			opts.onResult([], []);
		}
	}, 200);
}
