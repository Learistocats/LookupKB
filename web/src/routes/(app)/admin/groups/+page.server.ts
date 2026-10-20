import { redirect } from '@sveltejs/kit';
import type { Actions, PageServerLoad } from './$types';
import { adminLoad, adminMutate, formString, type AdminGroup } from '$lib/server/admin';

export const load: PageServerLoad = async ({ locals }) => {
	return { groups: await adminLoad<AdminGroup[]>(locals.accessToken, '/groups') };
};

export const actions: Actions = {
	create: async ({ locals, request }) => {
		const name = formString(await request.formData(), 'name');
		const out = await adminMutate(locals.accessToken, 'POST', '/groups', { name });
		if (!out.ok) return out.failure;
		redirect(303, `/admin/groups/${(out.data as AdminGroup).id}`);
	}
};
