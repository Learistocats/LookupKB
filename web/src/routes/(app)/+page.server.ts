// Home: the search page. Preloads the few most recently published entries so the
// page is useful before anything is typed; live results come from /search.
import type { PageServerLoad } from './$types';
import { apiFetch } from '$lib/server/api';
import type { SearchResult } from '$lib/entries';

export const load: PageServerLoad = async ({ locals }) => {
	if (!locals.accessToken) return { recent: [] };
	try {
		const res = await apiFetch(locals.accessToken, '/api/search?q=&limit=5');
		if (!res.ok) return { recent: [] };
		return { recent: ((await res.json()) as SearchResult).hits };
	} catch {
		return { recent: [] };
	}
};
