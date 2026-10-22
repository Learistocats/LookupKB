// Entry editor. The `save` action is the no-JS fallback for client autosave.
import { error, fail, redirect } from '@sveltejs/kit';
import type { Actions, PageServerLoad } from './$types';
import { apiFetch, apiJson } from '$lib/server/api';
import { saveEntry } from '$lib/server/entries';
import { NEW_ENTRY_ID } from '$lib/entries';

export type Draft = {
	id: string;
	slug: string;
	status: string;
	title: string | null;
	description: string | null;
	source_md: string | null;
	updated_at: string;
	tags?: string[];
};

export const load: PageServerLoad = async ({ locals, params }) => {
	// `/entries/new` has no row yet: it is created on its first non-blank save.
	if (params.id === NEW_ENTRY_ID) return { draft: null as Draft | null };

	const res = await apiFetch(locals.accessToken, `/api/drafts/${params.id}`);
	return { draft: await apiJson<Draft>(res, 'Entry') };
};

export const actions: Actions = {
	save: async ({ locals, params, request }) => {
		const form = await request.formData();
		const body = {
			title: emptyToNull(form.get('title')),
			description: emptyToNull(form.get('description')),
			source_md: emptyToNull(form.get('source_md'))
		};

		const result = await saveEntry(locals.accessToken, params.id, body);
		if (!result.ok) {
			if (result.status === 404) error(404, 'Entry not found');
			if (result.status === 400) return fail(400, { message: 'Nothing to save yet' });
			return fail(502, { message: `Save failed (API ${result.status})` });
		}
		// A new entry now exists: continue on its real URL.
		if (params.id === NEW_ENTRY_ID) redirect(303, `/entries/${result.draft.id}`);

		return { saved: true };
	}
};

// Blank fields persist as SQL NULL rather than an empty string.
function emptyToNull(v: FormDataEntryValue | null): string | null {
	if (typeof v !== 'string') return null;
	return v.length > 0 ? v : null;
}
