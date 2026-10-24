// API tokens: the new token's plaintext is returned by the create action exactly
// once and never stored by the BFF.
import { fail } from '@sveltejs/kit';
import type { Actions, PageServerLoad } from './$types';
import { apiFetch, apiMessage } from '$lib/server/api';
import { parseCreateTokenForm, type ApiToken, type CreatedToken } from '$lib/tokens';

export const load: PageServerLoad = async ({ locals }) => {
	if (!locals.accessToken) return { tokens: [] as ApiToken[], tokensError: null };
	try {
		const res = await apiFetch(locals.accessToken, '/api/tokens');
		if (!res.ok) return { tokens: [] as ApiToken[], tokensError: 'API tokens are unavailable.' };
		return { tokens: (await res.json()) as ApiToken[], tokensError: null };
	} catch {
		return { tokens: [] as ApiToken[], tokensError: 'API tokens are unavailable.' };
	}
};

export const actions: Actions = {
	create: async ({ request, locals }) => {
		const form = await request.formData();
		const name = form.get('name');
		const parsed = parseCreateTokenForm(name, form.get('expires_in_days'));
		if (!parsed.ok) {
			return fail(400, { action: 'create', name: String(name ?? ''), error: parsed.error });
		}
		const res = await apiFetch(locals.accessToken, '/api/tokens', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify(parsed.body)
		});
		if (!res.ok) {
			return fail(res.status, {
				action: 'create',
				name: parsed.body.name,
				error: await apiMessage(res, 'Could not create the token.')
			});
		}
		const created = (await res.json()) as CreatedToken;
		return { action: 'create', created: { name: created.name, token: created.token } };
	},

	revoke: async ({ request, locals }) => {
		const form = await request.formData();
		const id = String(form.get('id') ?? '');
		if (!id) return fail(400, { action: 'revoke', error: 'Missing token.' });
		const res = await apiFetch(locals.accessToken, `/api/tokens/${encodeURIComponent(id)}`, {
			method: 'DELETE'
		});
		if (!res.ok && res.status !== 404) {
			return fail(res.status, {
				action: 'revoke',
				error: await apiMessage(res, 'Could not revoke the token.')
			});
		}
		// 404: already revoked or not found — either way it no longer works.
		return { action: 'revoke', revoked: id };
	}
};
