// Browser-callable draft-delete proxy (LOO-58). Hard-deletes one of the caller's
// own draft/unpublished entries. Attaches the caller's bearer server-side and
// forwards to the Go API's DELETE /api/drafts/{id}. Same placement rationale as the
// publish/save proxies (off /api/* to dodge the dev Vite proxy).
import { error, type RequestHandler } from '@sveltejs/kit';
import { apiFetch } from '$lib/server/api';

export const DELETE: RequestHandler = async ({ locals, params }) => {
	const res = await apiFetch(locals.accessToken, `/api/drafts/${params.id}`, {
		method: 'DELETE'
	});

	// 404 covers "no such entry", "not yours" and "published" identically.
	if (res.status === 404) error(404, 'Entry not found');
	if (res.status === 400) error(400, 'Invalid entry id');
	if (!res.ok) error(502, `Delete failed (API ${res.status})`);

	return new Response(null, { status: 204 });
};
