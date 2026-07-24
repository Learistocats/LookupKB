// Browser-callable publish proxy (F5, AC-5.1–5.3). The confirm dialog POSTs the
// metadata here same-origin; this handler attaches the caller's bearer (never
// exposed to the browser) and forwards to the Go API's POST /api/documents/{id}/publish.
//
// Lives at /drafts/{id}/publish (NOT under /api/*) for the same reasons as the
// autosave proxy: SvelteKit forbids a +server beside the editor +page, and the dev
// Vite config proxies /api straight to the Go API.
import { json, error, type RequestHandler } from '@sveltejs/kit';
import { apiFetch } from '$lib/server/api';

export const POST: RequestHandler = async ({ locals, params, request }) => {
	const incoming = (await request.json()) as {
		title?: string;
		description?: string;
		tags?: string[];
	};
	const body = {
		title: incoming.title ?? '',
		description: incoming.description ?? '',
		tags: incoming.tags ?? []
	};

	const res = await apiFetch(locals.accessToken, `/api/documents/${params.id}/publish`, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(body)
	});

	// 404 covers "no such draft" and "not yours" identically (AC-2.3) — never leak which.
	if (res.status === 404) error(404, 'Draft not found');
	// 422 is the metadata gate: pass the per-field errors through unchanged (with the
	// 422 status) so the dialog can render them inline; the document stays a draft.
	if (res.status === 422) return json(await res.json(), { status: 422 });
	if (!res.ok) error(502, `Publish failed (API ${res.status})`);

	return json(await res.json());
};
