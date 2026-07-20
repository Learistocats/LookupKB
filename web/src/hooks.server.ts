// The OIDC gate. On every request this resolves the encrypted session cookie,
// silently refreshes an expired access token, and redirects unauthenticated
// users to Authentik (AC-1.1). Auth is skipped entirely when OIDC is not
// configured, so `pnpm dev` works without Authentik (mirrors the API).
import { redirect, type Handle } from '@sveltejs/kit';
import { getConfig, refreshSession } from '$lib/server/oidc';
import {
	SESSION_COOKIE,
	openSession,
	sealSession,
	isExpired,
	type Session
} from '$lib/server/session';

function cookieOptions(secure: boolean, maxAge: number) {
	return { path: '/', httpOnly: true, secure, sameSite: 'lax' as const, maxAge };
}

export const handle: Handle = async ({ event, resolve }) => {
	// Liveness probe is always open (used by the container healthcheck).
	if (event.url.pathname === '/healthz') {
		return resolve(event);
	}

	const cfg = getConfig();

	// Auth disabled (no issuer configured): serve openly, no user context.
	if (!cfg) {
		return resolve(event);
	}

	const secure = !cfg.allowInsecure;
	const isAuthRoute = event.url.pathname.startsWith('/auth/');
	const raw = event.cookies.get(SESSION_COOKIE);

	let session: Session | null = raw ? await openSession(raw, cfg.sessionSecret) : null;

	if (session && isExpired(session)) {
		// Silent refresh (AC-1.3). On failure, drop the session and fall through
		// to the redirect below rather than surfacing a raw error.
		try {
			session = await refreshSession(cfg, session);
			event.cookies.set(
				SESSION_COOKIE,
				await sealSession(session, cfg.sessionSecret, cfg.sessionTtl),
				cookieOptions(secure, cfg.sessionTtl)
			);
		} catch {
			event.cookies.delete(SESSION_COOKIE, { path: '/' });
			session = null;
		}
	}

	if (session) {
		event.locals.user = { sub: session.sub, email: session.email, groups: session.groups };
		// Stash the access token for server-side API proxying (never sent to the client).
		event.locals.accessToken = session.accessToken;
	}

	// Gate real app routes only (event.route.id is null for asset/404 misses).
	if (!session && !isAuthRoute && event.route.id !== null) {
		const returnTo = event.url.pathname + event.url.search;
		redirect(302, `/auth/login?returnTo=${encodeURIComponent(returnTo)}`);
	}

	return resolve(event);
};
