// Demonstrates the protected API path end-to-end: server-side, attach the
// caller's bearer and read GET /api/v1/me (AC-1.2/AC-1.4). Defensive so a
// transient API outage degrades gracefully instead of erroring the page.
import type { PageServerLoad } from './$types';
import { apiFetch } from '$lib/server/api';

export const load: PageServerLoad = async ({ locals }) => {
	if (!locals.accessToken) return { me: null, meError: null };
	try {
		const res = await apiFetch(locals.accessToken, '/api/v1/me');
		if (!res.ok) return { me: null, meError: `API /api/v1/me → ${res.status}` };
		return {
			me: (await res.json()) as { sub: string; email: string; groups: string[] },
			meError: null
		};
	} catch (e) {
		return { me: null, meError: e instanceof Error ? e.message : String(e) };
	}
};
