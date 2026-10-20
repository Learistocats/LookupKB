// Browser-callable autosave proxy (F2, AC-2.6). The editor's client-side autosave
// PUTs the working copy here same-origin; this handler attaches the caller's bearer
// (never exposed to the browser) and forwards to the Go API's PUT /api/drafts/{id}.
// For `/entries/new` the first non-blank save creates the draft (see saveEntry);
// the response carries its id so the editor can adopt it.
//
// It lives at /entries/{id}/save rather than under /api/* for two reasons: SvelteKit
// forbids a +server and a +page in the same directory (the editor +page owns
// /entries/{id}), and the dev Vite config proxies /api straight to the Go API, which
// would shadow a browser-facing /api/* route (same reason as /tags/search).
import { json, error, type RequestHandler } from '@sveltejs/kit';
import { saveEntry } from '$lib/server/entries';

export const PUT: RequestHandler = async ({ locals, params, request }) => {
	const incoming = (await request.json()) as {
		title?: string | null;
		description?: string | null;
		source_md?: string | null;
	};
	// Only the working-copy fields autosave touches; no validation gate (that is
	// publish-time). Blank strings persist as-is; the editor sends null for empty.
	const result = await saveEntry(locals.accessToken, params.id!, {
		title: incoming.title ?? null,
		description: incoming.description ?? null,
		source_md: incoming.source_md ?? null
	});
	if (!result.ok) {
		// 404 covers "no such draft" and "not yours" identically (ownership enforced
		// server-side, AC-2.3) — never leak which.
		if (result.status === 404) error(404, 'Entry not found');
		if (result.status === 400) error(400, 'Nothing to save yet');
		error(502, `Save failed (API ${result.status})`);
	}
	return json(result.draft);
};
