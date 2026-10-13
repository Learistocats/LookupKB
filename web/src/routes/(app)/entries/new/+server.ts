// "New entry": create an empty draft and open it in the editor. A plain form POST
// (works without JS) from the home page's + button and the history panel.
import { error, redirect, type RequestHandler } from '@sveltejs/kit';
import { apiFetch } from '$lib/server/api';

export const POST: RequestHandler = async ({ locals }) => {
	const res = await apiFetch(locals.accessToken, '/api/documents', { method: 'POST' });
	if (!res.ok)
		error(res.status === 401 ? 401 : 502, `Could not create an entry (API ${res.status})`);
	const draft = (await res.json()) as { id: string };
	redirect(303, `/entries/${draft.id}`);
};
