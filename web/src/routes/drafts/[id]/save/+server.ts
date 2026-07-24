// Browser-callable autosave proxy (F2, AC-2.6). The editor's client-side autosave
// PUTs the working copy here same-origin; this handler attaches the caller's bearer
// (never exposed to the browser) and forwards to the Go API's PUT /api/drafts/{id}.
//
// It lives at /drafts/{id}/save rather than under /api/* for two reasons: SvelteKit
// forbids a +server and a +page in the same directory (the editor +page owns
// /drafts/{id}), and the dev Vite config proxies /api straight to the Go API, which
// would shadow a browser-facing /api/* route (same reason as /tags/search).
import { json, error, type RequestHandler } from '@sveltejs/kit';
import { apiFetch } from '$lib/server/api';

export const PUT: RequestHandler = async ({ locals, params, request }) => {
	const incoming = (await request.json()) as {
		title?: string | null;
		description?: string | null;
		source_md?: string | null;
	};
	// Only the working-copy fields autosave touches; no validation gate (that is
	// publish-time). Blank strings persist as-is; the editor sends null for empty.
	const body = {
		title: incoming.title ?? null,
		description: incoming.description ?? null,
		source_md: incoming.source_md ?? null
	};

	const res = await apiFetch(locals.accessToken, `/api/drafts/${params.id}`, {
		method: 'PUT',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(body)
	});
	// 404 covers "no such draft" and "not yours" identically (ownership enforced
	// server-side, AC-2.3) — never leak which.
	if (res.status === 404) error(404, 'Draft not found');
	if (!res.ok) error(502, `Save failed (API ${res.status})`);

	return json(await res.json());
};
