// Thin client for the LookupKB API. The base URL is empty by default so calls
// are same-origin: in dev, Vite proxies /healthz to the Go API; in the compose
// stack, Caddy routes /healthz and /api to the API container. Override with
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
