// Personal Access Token pass-through (LOO-37). The API port is loopback-only in
// compose, so programmatic clients reach the PAT read surface through the web
// origin: /api/v1/* and /mcp are forwarded verbatim to the API. These helpers
// are pure so the forwarding rules are unit-tested.
//
// Rules:
//  - Only an `Authorization: Bearer lkb_pat_…` header is forwarded. Cookies (the
//    session) and any other credential are never sent upstream, so a browser
//    session can never be replayed through the proxy (no CSRF ride-along).
//  - The account routes (/api/v1/auth/*) are not proxied: the BFF's own
//    /login and /signup own them (and forward the client IP for rate limiting).
//  - The login redirect does not apply to these paths: a machine client gets the
//    API's 401 instead of an HTML redirect.

const PAT_BEARER = /^bearer\s+lkb_pat_/i;

/** True when the header carries a Personal Access Token. */
export function isPatAuthorization(header: string | null | undefined): boolean {
	return !!header && PAT_BEARER.test(header);
}

function under(path: string, prefix: string): boolean {
	return path === prefix || path.startsWith(prefix + '/');
}

/** True for the paths the PAT pass-through serves (and the login gate skips). */
export function isPatProxyPath(pathname: string): boolean {
	const p = pathname.toLowerCase();
	if (under(p, '/api/v1/auth')) return false;
	return under(p, '/api/v1') || under(p, '/mcp');
}

/** Request headers forwarded upstream (besides a PAT Authorization). */
const FORWARD_REQUEST = [
	'accept',
	'content-type',
	'if-none-match',
	'if-modified-since',
	// MCP streamable-HTTP transport (LOO-59).
	'mcp-session-id',
	'mcp-protocol-version',
	'last-event-id'
];

/** Response headers relayed back to the client. */
const FORWARD_RESPONSE = [
	'content-type',
	'cache-control',
	'etag',
	'last-modified',
	'retry-after',
	'x-ratelimit-limit',
	'x-ratelimit-remaining',
	'x-ratelimit-reset',
	'x-request-id',
	'www-authenticate',
	'mcp-session-id'
];

/** Build the upstream request headers: allowlisted ones plus a PAT, nothing else. */
export function upstreamHeaders(incoming: Headers): Headers {
	const out = new Headers();
	for (const name of FORWARD_REQUEST) {
		const v = incoming.get(name);
		if (v !== null) out.set(name, v);
	}
	const authz = incoming.get('authorization');
	if (isPatAuthorization(authz)) out.set('authorization', authz!);
	return out;
}

/** Pick the response headers to relay back. */
export function downstreamHeaders(upstream: Headers): Headers {
	const out = new Headers();
	for (const name of FORWARD_RESPONSE) {
		const v = upstream.get(name);
		if (v !== null) out.set(name, v);
	}
	return out;
}
