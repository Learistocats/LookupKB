// Expose the signed-in user (resolved by hooks.server.ts) to every page. Only
// non-sensitive projection fields cross to the client — never a token.
import type { LayoutServerLoad } from './$types';

export const load: LayoutServerLoad = ({ locals }) => {
	return { user: locals.user ?? null };
};
