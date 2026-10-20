// Entry editor. All API calls go server-side with the caller's bearer (never
// exposed to the browser). `load` reads the working copy back from Postgres —
// reloading this page is the round-trip persistence proof — and the `save`
// action is the no-JS fallback for autosave.
import { error, fail, redirect } from '@sveltejs/kit';
import type { Actions, PageServerLoad } from './$types';
import { apiFetch } from '$lib/server/api';
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
	// `/entries/new` is an entry with no row yet: it is created on its first
	// non-blank save, so opening the editor and leaving never leaves an empty draft.
	if (params.id === NEW_ENTRY_ID) return { draft: null as Draft | null };

	const res = await apiFetch(locals.accessToken, `/api/drafts/${params.id}`);
	// 404 covers both "no such draft" and "not yours" — the API never leaks which
	// (ownership is enforced server-side, AC-2.3).
	if (res.status === 404) error(404, 'Entry not found');
	if (!res.ok) error(502, `Could not load this entry (API ${res.status})`);
	return { draft: (await res.json()) as Draft };
};

export const actions: Actions = {
	// Persist the working copy in place (AC-2.2): title and source are optional and
	// empty/partial metadata saves with no validation gate (publish-time is M1).
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

		return { saved: true, draft: result.draft as unknown as Draft };
	}
};

// emptyToNull maps a missing/blank form field to null so empty metadata persists
// as SQL NULL rather than an empty string.
function emptyToNull(v: FormDataEntryValue | null): string | null {
	if (typeof v !== 'string') return null;
	return v.length > 0 ? v : null;
}
