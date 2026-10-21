// Browser-callable proxy for granting a tag to a group's policy (LOO-56). The page
// posts here same-origin; this attaches the bearer server-side and relays the API's
// status/body, including 409 needs_confirmation for the confirm-and-retry flow.
// Not under /api/* because the dev Vite config proxies /api straight to Go.
import type { RequestHandler } from '@sveltejs/kit';
import { adminProxy } from '$lib/server/admin';

export const POST: RequestHandler = async ({ locals, params, request }) => {
	let incoming: { tag?: unknown; confirm_protect?: unknown } = {};
	try {
		incoming = await request.json();
	} catch {
		// treated as an empty tag: the API answers 422
	}
	return adminProxy(
		locals.accessToken,
		'POST',
		`/groups/${encodeURIComponent(params.id ?? '')}/tags`,
		{
			tag: typeof incoming.tag === 'string' ? incoming.tag : '',
			confirm_protect: incoming.confirm_protect === true
		}
	);
};
