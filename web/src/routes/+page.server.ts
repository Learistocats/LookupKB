// Demonstrates the protected API path end-to-end: server-side, attach the
// caller's bearer and read GET /api/v1/me (AC-1.2/AC-1.4), plus the API's
// liveness (fetched server-side: the browser has no direct route to the API).
// Defensive so a transient API outage degrades gracefully instead of erroring.
import { error, redirect } from '@sveltejs/kit';
import type { Actions, PageServerLoad } from './$types';
import { apiFetch } from '$lib/server/api';
import { getHealth, type Health } from '$lib/api';

type HealthState = { ok: true; health: Health } | { ok: false; message: string };

async function apiHealth(): Promise<HealthState> {
	try {
		return { ok: true, health: await getHealth((path) => apiFetch(undefined, String(path))) };
	} catch (e) {
		return { ok: false, message: e instanceof Error ? e.message : String(e) };
	}
}

export const load: PageServerLoad = async ({ locals }) => {
	const health = await apiHealth();
	if (!locals.accessToken) return { me: null, meError: null, health };
	try {
		const res = await apiFetch(locals.accessToken, '/api/v1/me');
		if (!res.ok) return { me: null, meError: `API /api/v1/me → ${res.status}`, health };
		return {
			me: (await res.json()) as { id: string; email: string; groups: string[] },
			meError: null,
			health
		};
	} catch (e) {
		return { me: null, meError: e instanceof Error ? e.message : String(e), health };
	}
};

export const actions: Actions = {
	// Create a new draft (AC-2.1) and jump straight into its editor. The bearer is
	// attached server-side; the browser never sees the token. The new draft's id
	// comes back from the API and drives the redirect.
	createDraft: async ({ locals }) => {
		const res = await apiFetch(locals.accessToken, '/api/documents', { method: 'POST' });
		if (!res.ok) {
			error(res.status === 401 ? 401 : 502, `Could not create draft (API ${res.status})`);
		}
		const draft = (await res.json()) as { id: string };
		redirect(303, `/drafts/${draft.id}`);
	}
};
