// Super-admin helpers. The API re-checks the role on every /api/admin call; these
// just map statuses.
import { error, fail, json } from '@sveltejs/kit';
import { apiFetch, apiMessage } from './api';
import type { TagGrant } from '$lib/admin';

/** Shapes returned by /api/admin/*. */
export interface AdminGroup {
	id: string;
	name: string;
	created_at: string;
	member_count?: number;
	tag_count?: number;
}

export interface AdminUser {
	id: string;
	email: string;
	display_name: string | null;
	is_super_admin: boolean;
}

export interface AdminGroupDetail extends AdminGroup {
	members: AdminUser[];
	tags: TagGrant[];
}

const FORBIDDEN = 'You do not have access to this page.';

/** adminLoad GETs an admin resource for a page load, mapping 403/404 to error pages. */
export async function adminLoad<T>(token: string | undefined, path: string): Promise<T> {
	const res = await apiFetch(token, `/api/admin${path}`);
	if (res.status === 401 || res.status === 403) error(403, FORBIDDEN);
	if (res.status === 404) error(404, 'Not found');
	if (!res.ok) error(502, `Could not load (API ${res.status})`);
	return (await res.json()) as T;
}

/**
 * adminMutate sends a JSON mutation for a form action. A failure becomes
 * fail(status, { message }) carrying the API's error text (or `messages[status]`
 * when given), so the page can show it inline; success returns the parsed body (or
 * null for 204).
 */
export async function adminMutate(
	token: string | undefined,
	method: string,
	path: string,
	body?: unknown,
	messages: Partial<Record<number, string>> = {}
) {
	const res = await adminRequest(token, method, path, body);
	if (res.status === 401 || res.status === 403) error(403, FORBIDDEN);
	if (!res.ok) {
		const message = messages[res.status] ?? (await apiMessage(res));
		return { ok: false as const, failure: fail(res.status, { message }) };
	}
	const data: unknown = res.status === 204 ? null : await res.json();
	return { ok: true as const, data };
}

/**
 * adminProxy forwards a browser JSON call (tag policy) to the API and relays the
 * status + JSON body unchanged — including 409 needs_confirmation, which the
 * client-side confirm flow ($lib/admin) depends on.
 */
export async function adminProxy(
	token: string | undefined,
	method: string,
	path: string,
	body?: unknown
): Promise<Response> {
	const res = await adminRequest(token, method, path, body);
	if (res.status === 401 || res.status === 403) error(403, FORBIDDEN);
	if (res.status === 204) return new Response(null, { status: 204 });
	if (res.status >= 500) error(502, `Request failed (API ${res.status})`);
	let payload: unknown = {};
	try {
		payload = await res.json();
	} catch {
		// empty/non-JSON body: relay the status alone
	}
	return json(payload, { status: res.status });
}

function adminRequest(
	token: string | undefined,
	method: string,
	path: string,
	body?: unknown
): Promise<Response> {
	return apiFetch(token, `/api/admin${path}`, {
		method,
		headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
		body: body === undefined ? undefined : JSON.stringify(body)
	});
}

/** formString reads a trimmed string form field ('' when missing). */
export function formString(form: FormData, key: string): string {
	const v = form.get(key);
	return typeof v === 'string' ? v.trim() : '';
}
