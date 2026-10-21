// Tag helpers for the authoring UI (F6). Normalization and validation mirror the
// Go server rules (internal/tags) for immediate client feedback, but the server is
// always authoritative — never trust these counts at publish (06-security.md §6).
// Logic lives here (not inline in the component) so it is unit-testable and the
// component stays thin.

export interface TagLimits {
	minTags: number;
	maxTags: number;
	minLen: number;
	maxLen: number;
}

/** defaultTagLimits mirror the server DefaultLimits (00-overview.md §6). */
export const defaultTagLimits: TagLimits = { minTags: 1, maxTags: 8, minLen: 2, maxLen: 32 };

/**
 * A tag suggestion from the autocomplete endpoint. `protected` is true when a
 * group tag policy grants the tag (LOO-33): an entry carrying it is visible only
 * to members of those groups (and its author). Protected tags the caller has no
 * access to are never suggested.
 */
export interface TagSuggestion {
	slug: string;
	label: string;
	usage_count: number;
	protected: boolean;
}

/**
 * normalizeTag maps arbitrary input to a canonical lowercase-kebab slug, mirroring
 * the Go Normalize: trim, lowercase, collapse every run of non-(letter|digit) to a
 * single hyphen (Unicode letters kept), and strip leading/trailing hyphens.
 */
export function normalizeTag(raw: string): string {
	return raw
		.trim()
		.toLowerCase()
		.replace(/[^\p{L}\p{N}]+/gu, '-')
		.replace(/^-+|-+$/g, '');
}

/**
 * validateTags normalizes + de-dupes the input and returns the canonical set plus
 * any human-readable violations (empty when valid), for inline feedback. Rune
 * length is counted with the spread operator so multi-byte tags are measured fairly.
 */
export function validateTags(
	raw: string[],
	limits: TagLimits = defaultTagLimits
): { normalized: string[]; errors: string[] } {
	const seen = new Set<string>();
	const normalized: string[] = [];
	const errors: string[] = [];

	for (const r of raw) {
		const slug = normalizeTag(r);
		if (!slug) {
			errors.push(`"${r}" is not a valid tag`);
			continue;
		}
		if (seen.has(slug)) continue;
		seen.add(slug);
		normalized.push(slug);

		const len = [...slug].length;
		if (len < limits.minLen || len > limits.maxLen) {
			errors.push(`tag "${slug}" must be ${limits.minLen}–${limits.maxLen} characters`);
		}
	}

	if (normalized.length < limits.minTags) {
		errors.push(`at least ${limits.minTags} tag(s) required`);
	}
	if (normalized.length > limits.maxTags) {
		errors.push(`at most ${limits.maxTags} tags allowed`);
	}

	return { normalized, errors };
}

/**
 * searchTags calls the server-side autocomplete proxy (which attaches the bearer
 * and forwards to the Go API). fetchFn is injectable for tests. Path is /tags/search
 * (NOT /api/*) to avoid the dev-time Vite /api proxy that forwards straight to Go.
 */
export async function searchTags(
	q: string,
	fetchFn: typeof fetch = fetch
): Promise<TagSuggestion[]> {
	const res = await fetchFn(`/tags/search?q=${encodeURIComponent(q)}`);
	if (!res.ok) throw new Error(`tag search failed (${res.status})`);
	return (await res.json()) as TagSuggestion[];
}

/**
 * resolveProtected looks up, for each slug, whether it is a protected tag the
 * caller can see, using the autocomplete endpoint and an exact slug match. A slug
 * that is not returned (a brand-new tag, or one hidden from the caller) resolves
 * to false — the server's publish gate stays authoritative either way.
 */
export async function resolveProtected(
	slugs: string[],
	fetchFn: typeof fetch = fetch
): Promise<Record<string, boolean>> {
	const entries = await Promise.all(
		slugs.map(async (slug) => {
			const hits = await searchTags(slug, fetchFn);
			return [slug, hits.some((h) => h.slug === slug && h.protected)] as const;
		})
	);
	return Object.fromEntries(entries);
}

/** debounce delays fn until `ms` have elapsed since the last call (typeahead). */
export function debounce<A extends unknown[]>(
	fn: (...args: A) => void,
	ms: number
): (...args: A) => void {
	let timer: ReturnType<typeof setTimeout> | undefined;
	return (...args: A) => {
		clearTimeout(timer);
		timer = setTimeout(() => fn(...args), ms);
	};
}
