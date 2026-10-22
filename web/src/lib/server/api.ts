// Calls the Go API with the caller's bearer token (from the session, resolved in
// hooks.server.ts); the token never reaches the browser.
//
// Browser-callable proxies (+server.ts) live at /entries/*, /tags/search, etc. and
// never under /api/*: the dev Vite config proxies /api straight to the Go API, which
// would shadow them.
import { error } from '@sveltejs/kit';
import { env } from '$env/dynamic/private';

/** apiBase is the internal address of the Go API (Docker service name in compose). */
export function apiBase(): string {
	return env.API_INTERNAL_BASE?.trim() || 'http://api:8080';
}

export function apiFetch(
	accessToken: string | undefined,
	path: string,
	init: RequestInit = {}
): Promise<Response> {
	const headers = new Headers(init.headers);
	if (accessToken) headers.set('Authorization', `Bearer ${accessToken}`);
	return fetch(`${apiBase()}${path}`, { ...init, headers });
}

/**
 * apiOk throws a SvelteKit error for a failed API response: 404 -> 404 (the API
 * answers "missing" and "not yours" identically, so we do too), anything else -> 502.
 */
export function apiOk(res: Response, what: string): void {
	if (res.status === 404) error(404, `${what} not found`);
	if (!res.ok) error(502, `${what} failed (API ${res.status})`);
}

/** apiJson is apiOk followed by parsing the JSON body. */
export async function apiJson<T>(res: Response, what: string): Promise<T> {
	apiOk(res, what);
	return (await res.json()) as T;
}

/** apiMessage extracts the API's `error` text, falling back to `fallback`. */
export async function apiMessage(
	res: Response,
	fallback = `Request failed (${res.status})`
): Promise<string> {
	try {
		const b = (await res.json()) as { error?: string };
		if (b.error) return b.error;
	} catch {
		// non-JSON body
	}
	return fallback;
}
