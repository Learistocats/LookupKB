// Thin client for the LookupKB API health probe. The home page calls it
// server-side with apiFetch (the browser has no direct route to the API); the
// base URL is empty so a same-origin fetchFn can be passed in tests. Override with
// PUBLIC_API_BASE only for unusual split-origin deployments.
import { env } from '$env/dynamic/public';

const base = env.PUBLIC_API_BASE ?? '';

export type Health = {
	status: string;
	service: string;
	version: string;
};

export async function getHealth(fetchFn: typeof fetch = fetch): Promise<Health> {
	const res = await fetchFn(`${base}/healthz`);
	if (!res.ok) {
		throw new Error(`API /healthz returned ${res.status}`);
	}
	return (await res.json()) as Health;
}
