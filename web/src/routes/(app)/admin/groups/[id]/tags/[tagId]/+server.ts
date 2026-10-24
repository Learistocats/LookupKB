// Browser-callable proxy for removing a tag from a group's policy. Relays
// 409 needs_confirmation when this is the tag's last grant; the client retries with
// ?confirm_unprotect=true after the admin confirms.
import type { RequestHandler } from '@sveltejs/kit';
import { adminProxy } from '$lib/server/admin';

export const DELETE: RequestHandler = async ({ locals, params, url }) => {
	const confirm = url.searchParams.get('confirm_unprotect') === 'true';
	const path =
		`/groups/${encodeURIComponent(params.id ?? '')}/tags/${encodeURIComponent(params.tagId ?? '')}` +
		(confirm ? '?confirm_unprotect=true' : '');
	return adminProxy(locals.accessToken, 'DELETE', path);
};
