import { describe, expect, it } from 'vitest';
import { downstreamHeaders, isPatAuthorization, isPatProxyPath, upstreamHeaders } from './patProxy';

describe('isPatAuthorization', () => {
	it('accepts only a bearer PAT', () => {
		expect(isPatAuthorization('Bearer lkb_pat_abcdefgh_secret')).toBe(true);
		expect(isPatAuthorization('bearer   lkb_pat_x')).toBe(true);
		expect(isPatAuthorization('Bearer eyJhbGciOi.jwt.sig')).toBe(false);
		expect(isPatAuthorization('Basic lkb_pat_x')).toBe(false);
		expect(isPatAuthorization('')).toBe(false);
		expect(isPatAuthorization(null)).toBe(false);
	});
});

describe('isPatProxyPath', () => {
	it('covers the versioned API and MCP, never the account routes', () => {
		expect(isPatProxyPath('/api/v1/me')).toBe(true);
		expect(isPatProxyPath('/api/v1')).toBe(true);
		expect(isPatProxyPath('/API/V1/tags')).toBe(true);
		expect(isPatProxyPath('/mcp')).toBe(true);
		expect(isPatProxyPath('/mcp/stream')).toBe(true);
		expect(isPatProxyPath('/api/v1/auth/login')).toBe(false);
		expect(isPatProxyPath('/api/v1/auth')).toBe(false);
		expect(isPatProxyPath('/api/v1x')).toBe(false);
		expect(isPatProxyPath('/mcpx')).toBe(false);
		expect(isPatProxyPath('/api/tokens')).toBe(false);
		expect(isPatProxyPath('/settings')).toBe(false);
	});
});

describe('upstreamHeaders', () => {
	it('forwards a PAT and allowlisted headers but never cookies', () => {
		const h = upstreamHeaders(
			new Headers({
				authorization: 'Bearer lkb_pat_abcdefgh_s',
				cookie: 'lkb_session.0=secret',
				accept: 'text/plain',
				'content-type': 'application/json',
				'mcp-session-id': 'sess',
				'x-forwarded-for': '1.2.3.4',
				host: 'kb.example.com'
			})
		);
		expect(h.get('authorization')).toBe('Bearer lkb_pat_abcdefgh_s');
		expect(h.get('accept')).toBe('text/plain');
		expect(h.get('content-type')).toBe('application/json');
		expect(h.get('mcp-session-id')).toBe('sess');
		expect(h.get('cookie')).toBeNull();
		expect(h.get('x-forwarded-for')).toBeNull();
		expect(h.get('host')).toBeNull();
	});

	it('drops a non-PAT Authorization', () => {
		const h = upstreamHeaders(new Headers({ authorization: 'Bearer eyJ.session.jwt' }));
		expect(h.get('authorization')).toBeNull();
	});
});

describe('downstreamHeaders', () => {
	it('relays rate-limit and caching headers only', () => {
		const h = downstreamHeaders(
			new Headers({
				'content-type': 'application/json',
				'x-ratelimit-remaining': '3',
				'retry-after': '30',
				etag: '"v1"',
				'set-cookie': 'x=y',
				server: 'fasthttp'
			})
		);
		expect(h.get('content-type')).toBe('application/json');
		expect(h.get('x-ratelimit-remaining')).toBe('3');
		expect(h.get('retry-after')).toBe('30');
		expect(h.get('etag')).toBe('"v1"');
		expect(h.get('set-cookie')).toBeNull();
		expect(h.get('server')).toBeNull();
	});

	it('relays the public API negotiation and HTML boxing headers (LOO-38)', () => {
		const h = downstreamHeaders(
			new Headers({
				'content-type': 'text/html; charset=utf-8',
				vary: 'Accept',
				'content-security-policy': "default-src 'none'; sandbox",
				'x-content-type-options': 'nosniff'
			})
		);
		expect(h.get('vary')).toBe('Accept');
		expect(h.get('content-security-policy')).toContain('sandbox');
		expect(h.get('x-content-type-options')).toBe('nosniff');
	});
});
