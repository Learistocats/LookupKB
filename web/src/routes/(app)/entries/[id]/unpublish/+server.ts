// Browser-callable unpublish proxy (F5, AC-5.6). Reverts a published document to
// author-only visibility. Attaches the caller's bearer server-side and forwards to
// the Go API's POST /api/documents/{id}/unpublish. Same placement rationale as the
// publish/save proxies (off /api/* to dodge the dev Vite proxy).
import { json, error, type RequestHandler } from '@sveltejs/kit';
import { apiFetch } from '$lib/server/api';

export const POST: RequestHandler = async ({ locals, params }) => {
	const res = await apiFetch(locals.accessToken, `/api/documents/${params.id}/unpublish`, {
		method: 'POST'
	});

	// 404 covers "no such doc", "not yours" and "not currently published" identically.
	if (res.status === 404) error(404, 'Document not found');
	if (!res.ok) error(502, `Unpublish failed (API ${res.status})`);

	return json(await res.json());
};
