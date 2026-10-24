// One group: rename/delete, members (add by email / remove). The tag policy is
// changed from the browser through the JSON proxies under ./tags so the
// confirm-and-retry flow can run client-side ($lib/admin).
import { redirect } from '@sveltejs/kit';
import type { Actions, PageServerLoad } from './$types';
import { adminLoad, adminMutate, formString, type AdminGroupDetail } from '$lib/server/admin';

export const load: PageServerLoad = async ({ locals, params, depends }) => {
	depends('admin:group');
	const path = `/groups/${encodeURIComponent(params.id)}`;
	return { group: await adminLoad<AdminGroupDetail>(locals.accessToken, path) };
};

export const actions: Actions = {
	rename: async ({ locals, params, request }) => {
		const name = formString(await request.formData(), 'name');
		const out = await adminMutate(
			locals.accessToken,
			'PATCH',
			`/groups/${encodeURIComponent(params.id)}`,
			{ name }
		);
		if (!out.ok) return out.failure;
		return { renamed: true };
	},

	delete: async ({ locals, params }) => {
		const out = await adminMutate(
			locals.accessToken,
			'DELETE',
			`/groups/${encodeURIComponent(params.id)}`
		);
		if (!out.ok) return out.failure;
		redirect(303, '/admin/groups');
	},

	addMember: async ({ locals, params, request }) => {
		const email = formString(await request.formData(), 'email');
		const out = await adminMutate(
			locals.accessToken,
			'POST',
			`/groups/${encodeURIComponent(params.id)}/members`,
			{ email },
			// A 404 here means "no account with that email" (the group itself loaded).
			{ 404: 'No account with that email.' }
		);
		if (!out.ok) return out.failure;
		return { added: true };
	},

	removeMember: async ({ locals, params, request }) => {
		const userId = formString(await request.formData(), 'user_id');
		const out = await adminMutate(
			locals.accessToken,
			'DELETE',
			`/groups/${encodeURIComponent(params.id)}/members/${encodeURIComponent(userId)}`
		);
		if (!out.ok) return out.failure;
		return { removed: true };
	}
};
