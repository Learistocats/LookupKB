// Email/password sign-in. The form posts here; the API verifies the credentials
// and the returned token is sealed into the session cookie (never sent to JS).
import { fail, redirect } from '@sveltejs/kit';
import type { Actions, PageServerLoad } from './$types';
import { exchangeCredentials, getAuthConfig, safeReturnTo } from '$lib/server/auth';

export const load: PageServerLoad = ({ locals, url }) => {
	if (locals.user) redirect(303, safeReturnTo(url.searchParams.get('returnTo')));
	return { returnTo: safeReturnTo(url.searchParams.get('returnTo')) };
};

export const actions: Actions = {
	default: async ({ request, cookies, getClientAddress }) => {
		const cfg = getAuthConfig();
		if (!cfg) return fail(503, { email: '', error: 'Authentication is not configured.' });

		const form = await request.formData();
		const email = String(form.get('email') ?? '').trim();
		const password = String(form.get('password') ?? '');
		const returnTo = safeReturnTo(String(form.get('returnTo') ?? '/'));

		if (!email || !password) {
			return fail(400, { email, error: 'Enter your email and password.' });
		}

		const result = await exchangeCredentials(
			cfg,
			cookies,
			'login',
			{ email, password },
			getClientAddress()
		);
		if (!result.ok) return fail(result.status, { email, error: result.message });

		redirect(303, returnTo);
	}
};
