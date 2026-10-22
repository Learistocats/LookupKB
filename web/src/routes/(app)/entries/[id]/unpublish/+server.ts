import { json, type RequestHandler } from '@sveltejs/kit';
import { apiFetch, apiJson } from '$lib/server/api';

export const POST: RequestHandler = async ({ locals, params }) => {
	const res = await apiFetch(locals.accessToken, `/api/documents/${params.id}/unpublish`, {
		method: 'POST'
	});

	return json(await apiJson(res, 'Document'));
};
