// Expose the signed-in user and UI preferences (resolved by hooks.server.ts) to
// every page. Only non-sensitive fields cross to the client — never a token.
import type { LayoutServerLoad } from './$types';
import { THEME_COOKIE, parseTheme } from '$lib/theme';

export const load: LayoutServerLoad = ({ locals, cookies }) => {
	return {
		user: locals.user ?? null,
		theme: parseTheme(cookies.get(THEME_COOKIE)),
		panelOpen: cookies.get('lkb_panel') === '1'
	};
};
