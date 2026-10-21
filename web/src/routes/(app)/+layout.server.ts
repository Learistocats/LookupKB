// Signed-in app shell: loads the caller's own entries for the history panel
// (drafts first) and whether they are a super-admin (to show the Admin link).
// `depends('app:entries')` lets the editor refresh the panel after a save or
// publish with invalidate('app:entries').
import type { LayoutServerLoad } from './$types';
import { apiFetch } from '$lib/server/api';
import type { EntrySummary } from '$lib/entries';

async function loadEntries(token: string): Promise<EntrySummary[]> {
	try {
		const res = await apiFetch(token, '/api/documents/mine');
		if (!res.ok) return [];
		return (await res.json()) as EntrySummary[];
	} catch {
		// The panel is secondary: an API hiccup must not break the page.
		return [];
	}
}

// isSuperAdmin only decides whether the Admin link is shown; the API re-checks
// the role on every /api/admin request, so a stale or forged value grants nothing.
async function loadIsSuperAdmin(token: string): Promise<boolean> {
	try {
		const res = await apiFetch(token, '/api/v1/me');
		if (!res.ok) return false;
		return ((await res.json()) as { is_super_admin?: boolean }).is_super_admin === true;
	} catch {
		return false;
	}
}

export const load: LayoutServerLoad = async ({ locals, depends }) => {
	depends('app:entries');
	if (!locals.accessToken) return { entries: [] as EntrySummary[], isSuperAdmin: false };
	const [entries, isSuperAdmin] = await Promise.all([
		loadEntries(locals.accessToken),
		loadIsSuperAdmin(locals.accessToken)
	]);
	return { entries, isSuperAdmin };
};
