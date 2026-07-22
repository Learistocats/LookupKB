// Server-side autocomplete proxy (F6, AC-6.1). The browser typeahead calls this
// same-origin; it attaches the caller's bearer (never exposed to the browser) and
// forwards to the Go API's GET /api/v1/tags. It lives at /tags/search rather than
// /api/tags because the dev Vite config proxies /api straight to the Go API, which
// would shadow a browser-facing /api/* route.
import { json, error, type RequestHandler } from '@sveltejs/kit';
import { apiFetch } from '$lib/server/api';

export const GET: RequestHandler = async ({ locals, url }) => {
	const q = url.searchParams.get('q') ?? '';
	const limit = url.searchParams.get('limit');

	let path = `/api/v1/tags?q=${encodeURIComponent(q)}`;
	if (limit) path += `&limit=${encodeURIComponent(limit)}`;

	const res = await apiFetch(locals.accessToken, path);
	if (!res.ok) error(502, `tag search failed (API ${res.status})`);

	return json(await res.json());
};
