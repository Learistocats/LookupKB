// Browser-callable search proxy for the home page's live results. Attaches the
// caller's bearer server-side and forwards to GET /api/search, which injects the
// access filter (the browser never reaches Meilisearch or the API directly).
import { json, type RequestHandler } from '@sveltejs/kit';
import { apiFetch } from '$lib/server/api';

export const GET: RequestHandler = async ({ locals, url }) => {
	const params = new URLSearchParams();
	params.set('q', url.searchParams.get('q') ?? '');
	params.set('limit', url.searchParams.get('limit') ?? '20');
	const res = await apiFetch(locals.accessToken, `/api/search?${params}`);
	if (!res.ok) {
		return json(
			{ error: 'Search is unavailable right now.' },
			{ status: res.status === 401 ? 401 : 503 }
		);
	}
	return json(await res.json());
};
