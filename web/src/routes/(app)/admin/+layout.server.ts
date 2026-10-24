// Gate the admin area on the role reported by the API. This only avoids rendering
// an admin shell for non-admins; the API re-checks the role on every /api/admin
// request (and each page load maps its 403 to this same error).
import { error } from '@sveltejs/kit';
import type { LayoutServerLoad } from './$types';

export const load: LayoutServerLoad = async ({ parent }) => {
	const { isSuperAdmin } = await parent();
	if (!isSuperAdmin) error(403, 'You do not have access to this page.');
	return {};
};
