import { json, type RequestHandler } from '@sveltejs/kit';
import { apiFetch, apiJson } from '$lib/server/api';

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

	// 422 is the metadata gate: pass the per-field errors through for inline display.
	if (res.status === 422) return json(await res.json(), { status: 422 });

	return json(await apiJson(res, 'Draft'));
};
