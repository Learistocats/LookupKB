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

/** The id segment of an entry that does not exist yet (`/entries/new`). */
export const NEW_ENTRY_ID = 'new';

/** The editable working copy, as autosave sends it (blank fields are null). */
export type WorkingCopy = {
	title: string | null;
	description: string | null;
	source_md: string | null;
};

/**
 * isBlankEntry: nothing has been written yet. A new entry is only created on its
 * first non-blank save, so opening the editor and leaving never leaves an empty draft.
 */
export function isBlankEntry(c: WorkingCopy): boolean {
	return !c.title?.trim() && !c.description?.trim() && !c.source_md?.trim();
}
