import type { Actions, PageServerLoad } from './$types';
import { adminLoad, adminMutate, formString, type AdminUser } from '$lib/server/admin';

export const load: PageServerLoad = async ({ locals }) => {
	return { users: await adminLoad<AdminUser[]>(locals.accessToken, '/users') };
};

export const actions: Actions = {
	// Grant or revoke the super-admin role. The API refuses (409) to remove the
	// last super-admin; its message is shown inline.
	setSuperAdmin: async ({ locals, request }) => {
		const form = await request.formData();
		const userId = formString(form, 'user_id');
		const value = formString(form, 'value') === 'true';
		const out = await adminMutate(
			locals.accessToken,
			'PUT',
			`/users/${encodeURIComponent(userId)}/super-admin`,
			{ value }
		);
		if (!out.ok) return out.failure;
		return { updated: userId };
	}
};
