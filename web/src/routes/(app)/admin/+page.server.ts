// /admin has no overview of its own yet: send admins to the groups page.
import { redirect } from '@sveltejs/kit';
import type { PageServerLoad } from './$types';

export const load: PageServerLoad = () => {
	redirect(307, '/admin/groups');
};
