// Signed-in app shell: the caller's entries for the history panel.
// invalidate('app:entries') refreshes the panel after a save or publish.
import type { LayoutServerLoad } from './$types';
import { apiFetch } from '$lib/server/api';
import type { EntrySummary } from '$lib/entries';

export const load: LayoutServerLoad = async ({ locals, depends }) => {
	depends('app:entries');
	if (!locals.accessToken) return { entries: [] as EntrySummary[] };
	try {
		const res = await apiFetch(locals.accessToken, '/api/documents/mine');
		return { entries: res.ok ? ((await res.json()) as EntrySummary[]) : [] };
	} catch {
		// The panel is secondary: an API hiccup must not break the page.
		return { entries: [] as EntrySummary[] };
	}
};
