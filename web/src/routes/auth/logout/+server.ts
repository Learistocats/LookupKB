// Logout: clear the local session cookie and redirect to Authentik's
// RP-initiated end-session endpoint so the SSO session is revoked too
// (06-security.md §5). Accepts POST (CSRF-safer for a mutating action) and GET
// (convenience for a plain link).
import { redirect, type RequestHandler } from '@sveltejs/kit';
import { getConfig, buildLogoutUrl } from '$lib/server/oidc';
import { SESSION_COOKIE, openSession } from '$lib/server/session';

async function logout(cookies: Parameters<RequestHandler>[0]['cookies']): Promise<never> {
	const cfg = getConfig();
	const raw = cfg ? cookies.get(SESSION_COOKIE) : undefined;
	const session = raw && cfg ? await openSession(raw, cfg.sessionSecret) : null;

	cookies.delete(SESSION_COOKIE, { path: '/' });

	if (cfg) {
		// Revoke at Authentik; id_token_hint lets it skip the confirmation prompt.
		redirect(302, await buildLogoutUrl(cfg, session?.idToken));
	}
	redirect(302, '/');
}

export const POST: RequestHandler = ({ cookies }) => logout(cookies);
export const GET: RequestHandler = ({ cookies }) => logout(cookies);
