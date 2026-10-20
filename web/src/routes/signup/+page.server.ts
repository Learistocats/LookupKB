// Open self-service signup: create an email/password account via the API and
// start a session straight away.
import { fail, redirect } from '@sveltejs/kit';
import type { Actions, PageServerLoad } from './$types';
import { exchangeCredentials, getAuthConfig } from '$lib/server/auth';

export const load: PageServerLoad = ({ locals }) => {
	if (locals.user) redirect(303, '/');
	return {};
};

export const actions: Actions = {
	default: async ({ request, cookies, getClientAddress }) => {
		const cfg = getAuthConfig();
		if (!cfg) {
			return fail(503, { email: '', displayName: '', error: 'Authentication is not configured.' });
		}

		const form = await request.formData();
		const email = String(form.get('email') ?? '').trim();
		const displayName = String(form.get('display_name') ?? '').trim();
		const password = String(form.get('password') ?? '');
		const confirm = String(form.get('confirm') ?? '');

		if (password !== confirm) {
			return fail(400, { email, displayName, error: 'Passwords do not match.' });
		}

		const result = await exchangeCredentials(
			cfg,
			cookies,
			'signup',
			{ email, password, display_name: displayName },
			getClientAddress()
		);
		if (!result.ok) return fail(result.status, { email, displayName, error: result.message });

		redirect(303, '/');
	}
};
