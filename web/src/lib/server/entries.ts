// Server-side save of an entry's working copy, shared by the editor's autosave
// proxy and its no-JS form action. A new entry (`/entries/new`) has no row yet:
// its first save creates the draft and then writes the working copy, so an entry
// that is opened and left untouched is never persisted.
import { apiFetch } from './api';
import { NEW_ENTRY_ID, isBlankEntry, type WorkingCopy } from '$lib/entries';

export type SaveResult =
	{ ok: true; draft: { id: string } & Record<string, unknown> } | { ok: false; status: number };

export async function saveEntry(
	accessToken: string | undefined,
	id: string,
	body: WorkingCopy
): Promise<SaveResult> {
	if (id === NEW_ENTRY_ID) {
		// Nothing to persist: never create an empty draft.
		if (isBlankEntry(body)) return { ok: false, status: 400 };
		const created = await apiFetch(accessToken, '/api/documents', { method: 'POST' });
		if (!created.ok) return { ok: false, status: created.status };
		id = ((await created.json()) as { id: string }).id;
	}

	const res = await apiFetch(accessToken, `/api/drafts/${id}`, {
		method: 'PUT',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(body)
	});
	if (!res.ok) return { ok: false, status: res.status };
	return { ok: true, draft: await res.json() };
}
