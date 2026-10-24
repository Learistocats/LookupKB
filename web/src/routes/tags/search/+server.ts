import { json, type RequestHandler } from '@sveltejs/kit';
import { apiFetch, apiJson } from '$lib/server/api';

// Autocomplete proxy for the tag typeahead.
export const GET: RequestHandler = async ({ locals, url }) => {
	const q = url.searchParams.get('q') ?? '';
	const limit = url.searchParams.get('limit');

	let path = `/api/v1/tags?q=${encodeURIComponent(q)}`;
	if (limit) path += `&limit=${encodeURIComponent(limit)}`;

	return json(await apiJson(await apiFetch(locals.accessToken, path), 'Tag search'));
};
