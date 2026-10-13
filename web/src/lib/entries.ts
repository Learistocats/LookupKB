// Shapes shared by the app shell, history panel and search UI.

/** One of the caller's own entries, as listed in the history panel. */
export type EntrySummary = {
	id: string;
	slug: string;
	status: 'draft' | 'published' | 'unpublished';
	title: string | null;
	updated_at: string;
};

/** A sanitized search hit. title_html/snippet contain escaped text + <mark> only. */
export type SearchHit = {
	slug: string;
	title: string;
	title_html: string;
	description: string;
	snippet: string;
	tags: string[];
	author: string;
	published_at: string;
};

export type SearchResult = { hits: SearchHit[]; took_ms: number };
