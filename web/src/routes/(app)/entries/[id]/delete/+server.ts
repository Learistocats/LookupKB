import { error, type RequestHandler } from '@sveltejs/kit';
import { apiFetch, apiOk } from '$lib/server/api';

export const DELETE: RequestHandler = async ({ locals, params }) => {
	const res = await apiFetch(locals.accessToken, `/api/drafts/${params.id}`, {
		method: 'DELETE'
	});

	if (res.status === 400) error(400, 'Invalid entry id');
	apiOk(res, 'Entry');

	return new Response(null, { status: 204 });
};
