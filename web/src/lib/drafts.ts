// Draft-delete client logic. The editor topbar and the history panel both
// offer "Delete"; this module holds the rule for when it is offered and the call to
// the same-origin proxy (which attaches the bearer server-side). Kept out of the
// components so it is unit-testable. The SERVER is authoritative: it only deletes
// the caller's own draft/unpublished entries and 404s on anything else.

import { NEW_ENTRY_ID } from './entries';

/** The message the delete confirmation shows. */
export const DELETE_CONFIRM_MESSAGE = "Delete this draft? This can't be undone.";

/**
 * canDeleteEntry: a persisted entry that is not currently public. A published entry
 * must be unpublished first (the API refuses it); an unsaved `/entries/new` has
 * nothing to delete.
 */
export function canDeleteEntry(id: string | null | undefined, status: string): boolean {
	if (!id || id === NEW_ENTRY_ID) return false;
	return status === 'draft' || status === 'unpublished';
}

/**
 * deleteEntry sends DELETE to /entries/{id}/delete (see
 * lib/server/api.ts for why not /api/*). Resolves on success; throws on any non-OK status,
 * including 404 (gone, not yours, or published — the API never says which).
 */
export async function deleteEntry(id: string, fetchFn: typeof fetch = fetch): Promise<void> {
	const res = await fetchFn(`/entries/${encodeURIComponent(id)}/delete`, { method: 'DELETE' });
	if (res.status === 404) throw new Error('This entry no longer exists or can’t be deleted.');
	if (!res.ok) throw new Error(`delete failed (${res.status})`);
}
