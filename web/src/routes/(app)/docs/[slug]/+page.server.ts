// Read path: fetches the pre-rendered, sanitized HTML by slug; never renders here.
import type { PageServerLoad } from './$types';
import { apiFetch, apiJson } from '$lib/server/api';

// Published-safe projection: rendered HTML plus display metadata, no source_md/author_id.
export type PublishedDoc = {
	/** Present only when the caller wrote this entry (enables "Edit"). */
	id?: string;
	slug: string;
	title: string | null;
	description: string | null;
	author_display_name: string | null;
	published_at: string;
	tags: string[];
	rendered_html: string;
};

export const load: PageServerLoad = async ({ locals, params }) => {
	const res = await apiFetch(locals.accessToken, `/api/documents/${params.slug}`);
	return { doc: await apiJson<PublishedDoc>(res, 'Document') };
};
