// Private workspace: lists the caller's own drafts (AC-2.5). The API's GET
// /api/drafts is owner-scoped server-side, so this only ever returns the caller's
// drafts — no other user's (including admins) are visible. All API calls go
// server-side with the caller's bearer (never exposed to the browser).
import { error, redirect } from '@sveltejs/kit';
import type { Actions, PageServerLoad } from './$types';
import { apiFetch } from '$lib/server/api';

export type DraftListItem = {
	id: string;
	slug: string;
	status: string;
	title: string | null;
	updated_at: string;
};

export const load: PageServerLoad = async ({ locals }) => {
	const res = await apiFetch(locals.accessToken, '/api/drafts');
	if (!res.ok) error(502, `Could not load drafts (API ${res.status})`);
	return { drafts: (await res.json()) as DraftListItem[] };
};

export const actions: Actions = {
	// Create a new empty draft (AC-2.1) and jump straight into its editor — the
	// implicit-save model (no "New Document" form before typing).
	createDraft: async ({ locals }) => {
		const res = await apiFetch(locals.accessToken, '/api/documents', { method: 'POST' });
		if (!res.ok) {
			error(res.status === 401 ? 401 : 502, `Could not create draft (API ${res.status})`);
		}
		const draft = (await res.json()) as { id: string };
		redirect(303, `/drafts/${draft.id}`);
	}
};
