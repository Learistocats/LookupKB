// Forwards a PAT request from the web origin to the Go API and streams the
// response back (LOO-37). The forwarding rules live in ./patProxy (pure, tested);
// this is the thin I/O around them. The API alone authenticates and authorizes:
// a missing/non-PAT credential is simply not forwarded, so the API answers 401.
import type { RequestEvent } from '@sveltejs/kit';
import { apiBase } from './api';
import { downstreamHeaders, isPatProxyPath, upstreamHeaders } from './patProxy';

const NO_BODY_STATUS = new Set([101, 204, 205, 304]);

function jsonError(status: number, error: string): Response {
	return new Response(JSON.stringify({ error }), {
		status,
		headers: { 'content-type': 'application/json' }
	});
}

export async function forwardPatRequest({ request, url }: RequestEvent): Promise<Response> {
	if (!isPatProxyPath(url.pathname)) return jsonError(404, 'not found');

	const method = request.method.toUpperCase();
	const hasBody = method !== 'GET' && method !== 'HEAD';
	let upstream: Response;
	try {
		upstream = await fetch(`${apiBase()}${url.pathname}${url.search}`, {
			method,
			headers: upstreamHeaders(request.headers),
			body: hasBody ? await request.arrayBuffer() : undefined,
			redirect: 'manual'
		});
	} catch {
		return jsonError(502, 'upstream unavailable');
	}

	const body = method === 'HEAD' || NO_BODY_STATUS.has(upstream.status) ? null : upstream.body;
	return new Response(body, {
		status: upstream.status,
		headers: downstreamHeaders(upstream.headers)
	});
}
