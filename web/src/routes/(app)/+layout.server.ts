// Signed-in app shell: loads the caller's own entries for the history panel
// (drafts first). `depends('app:entries')` lets the editor refresh the panel
// after a save or publish with invalidate('app:entries').
import type { LayoutServerLoad } from './$types';
import { apiFetch } from '$lib/server/api';
import type { EntrySummary } from '$lib/entries';

export const load: LayoutServerLoad = async ({ locals, depends }) => {
	depends('app:entries');
	if (!locals.accessToken) return { entries: [] as EntrySummary[] };
	try {
		const res = await apiFetch(locals.accessToken, '/api/documents/mine');
		if (!res.ok) return { entries: [] as EntrySummary[] };
		return { entries: (await res.json()) as EntrySummary[] };
	} catch {
		// The panel is secondary: an API hiccup must not break the page.
		return { entries: [] as EntrySummary[] };
	}
};
