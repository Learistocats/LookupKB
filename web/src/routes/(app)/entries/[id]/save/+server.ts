import { json, error, type RequestHandler } from '@sveltejs/kit';
import { saveEntry } from '$lib/server/entries';

// The first non-blank save of `/entries/new` creates the draft; the response carries
// its id so the editor can adopt it.
export const PUT: RequestHandler = async ({ locals, params, request }) => {
	const incoming = (await request.json()) as {
		title?: string | null;
		description?: string | null;
		source_md?: string | null;
	};
	const result = await saveEntry(locals.accessToken, params.id!, {
		title: incoming.title ?? null,
		description: incoming.description ?? null,
		source_md: incoming.source_md ?? null
	});
	if (!result.ok) {
		if (result.status === 404) error(404, 'Entry not found');
		if (result.status === 400) error(400, 'Nothing to save yet');
		error(502, `Save failed (API ${result.status})`);
	}
	return json(result.draft);
};
