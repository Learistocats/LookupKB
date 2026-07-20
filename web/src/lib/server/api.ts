// Server-side helper for calling the Go API with the caller's bearer token. The
// token comes from the session (resolved in hooks.server.ts) and is attached
// here — it never reaches the browser. Used by server load functions / actions.
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
