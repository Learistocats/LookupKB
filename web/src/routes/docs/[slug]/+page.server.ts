// Published-document read view (LOO-18, F8). The hot read path: `load` fetches the
// server's pre-rendered, sanitized HTML by slug and hands it to the page. The API
// call runs server-side with the caller's bearer (never exposed to the browser); no
// browser-facing +server.ts proxy is needed because SSR `load` calls the API directly.
import { error } from '@sveltejs/kit';
import type { PageServerLoad } from './$types';
import { apiFetch } from '$lib/server/api';

// PublishedDoc is the published-safe projection the API returns — rendered HTML plus
// display metadata only. It deliberately carries no source_md/author_id.
export type PublishedDoc = {
	slug: string;
	title: string | null;
	description: string | null;
	author_display_name: string | null;
	published_at: string;
	tags: string[];
	rendered_html: string;
};

export const load: PageServerLoad = async ({ locals, params }) => {
	const res = await apiFetch(locals.accessToken, `/api/documents/${params.slug}`);
	// 404 covers "no such published doc", "not published yet", and "restricted & not
	// yours" — the API returns an identical 404 for all three so existence never leaks
	// (AC-8.4).
	if (res.status === 404) error(404, 'Document not found');
	if (!res.ok) error(502, `Could not load document (API ${res.status})`);
	return { doc: (await res.json()) as PublishedDoc };
};
