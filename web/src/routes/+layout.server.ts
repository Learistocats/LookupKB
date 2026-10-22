// Expose the signed-in user and UI preferences (resolved by hooks.server.ts) to
// every page. Only non-sensitive fields cross to the client — never a token.
import type { LayoutServerLoad } from './$types';
import { apiFetch } from '$lib/server/api';
import { PANEL_COOKIE, THEME_COOKIE, parseTheme } from '$lib/theme';

// Only decides whether the Admin link is shown; the API re-checks the role on every
// /api/admin request, so a stale or forged value grants nothing. Its own dependency
// key keeps the frequent invalidate('app:entries') from re-fetching it.
async function loadIsSuperAdmin(token: string | undefined): Promise<boolean> {
	if (!token) return false;
	try {
		const res = await apiFetch(token, '/api/v1/me');
		if (!res.ok) return false;
		return ((await res.json()) as { is_super_admin?: boolean }).is_super_admin === true;
	} catch {
		return false;
	}
}

export const load: LayoutServerLoad = async ({ locals, cookies, depends }) => {
	depends('app:me');
	return {
		user: locals.user ?? null,
		isSuperAdmin: await loadIsSuperAdmin(locals.accessToken),
		theme: parseTheme(cookies.get(THEME_COOKIE)),
		panelOpen: cookies.get(PANEL_COOKIE) === '1'
	};
};
