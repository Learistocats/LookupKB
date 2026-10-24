// The auth gate. On every request this resolves the encrypted session cookie and
// redirects unauthenticated users to /login. There is no refresh: once
// the API token expires the user signs in again. Auth is skipped entirely when no
// session secret is configured, so `pnpm dev` works without a backend (mirrors
// the API). Also sets the baseline security headers the edge proxy used to add.
import { redirect, type Handle } from '@sveltejs/kit';
import { getAuthConfig } from '$lib/server/auth';
import { THEME_COOKIE, parseTheme } from '$lib/theme';
import { isPatProxyPath } from '$lib/server/patProxy';
import {
	getSessionCookie,
	clearSessionCookie,
	openSession,
	isExpired,
	type Session
} from '$lib/server/session';

/** Routes reachable without a session. */
const PUBLIC_PATHS = new Set(['/login', '/signup', '/healthz']);

function withSecurityHeaders(response: Response, secure: boolean): Response {
	const h = response.headers;
	h.set('X-Content-Type-Options', 'nosniff');
	h.set('X-Frame-Options', 'DENY');
	h.set('Referrer-Policy', 'strict-origin-when-cross-origin');
	h.set('Permissions-Policy', 'geolocation=(), camera=(), microphone=(), interest-cohort=()');
	if (secure) h.set('Strict-Transport-Security', 'max-age=31536000; includeSubDomains');
	return response;
}

/** Render the saved theme into <html> so the first paint is already right. */
function themed(event: Parameters<Handle>[0]['event']) {
	const theme = parseTheme(event.cookies.get(THEME_COOKIE));
	if (theme === 'system') return {};
	return {
		transformPageChunk: ({ html }: { html: string }) =>
			html.replace('<html lang="en"', `<html lang="en" data-theme="${theme}"`)
	};
}

export const handle: Handle = async ({ event, resolve }) => {
	// Liveness probe is always open (used by the container healthcheck).
	if (event.url.pathname === '/healthz') {
		return resolve(event);
	}

	const cfg = getAuthConfig();

	// Auth disabled (no secret configured): serve openly, no user context.
	if (!cfg) {
		return withSecurityHeaders(await resolve(event, themed(event)), false);
	}

	const raw = getSessionCookie(event.cookies);
	let session: Session | null = raw ? await openSession(raw, cfg.sessionSecret) : null;

	if (session && isExpired(session)) {
		clearSessionCookie(event.cookies);
		session = null;
	}

	if (session) {
		event.locals.user = { id: session.userId, email: session.email };
		// Stash the access token for server-side API proxying (never sent to the client).
		event.locals.accessToken = session.accessToken;
	}

	// Gate real app routes only (event.route.id is null for asset/404 misses). The
	// PAT pass-through (/api/v1/*, /mcp) is for machine clients: never redirected,
	// the API answers 401 itself when no Personal Access Token is presented.
	if (
		!session &&
		!PUBLIC_PATHS.has(event.url.pathname) &&
		!isPatProxyPath(event.url.pathname) &&
		event.route.id !== null
	) {
		const returnTo = event.url.pathname + event.url.search;
		redirect(302, `/login?returnTo=${encodeURIComponent(returnTo)}`);
	}

	return withSecurityHeaders(await resolve(event, themed(event)), cfg.secure);
};
