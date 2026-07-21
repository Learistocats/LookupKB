// OIDC redirect URI. Validates state, exchanges the code (+PKCE verifier) for
// tokens, and seals the session into an encrypted HttpOnly cookie. On any
// failure the user is bounced back to a fresh login rather than shown a raw
// error (AC-1.3 spirit: never surface protocol errors).
import { redirect, error, type RequestHandler } from '@sveltejs/kit';
import { getConfig, completeLogin } from '$lib/server/oidc';
import { setSessionCookie, TX_COOKIE, openTransaction, sealSession } from '$lib/server/session';

export const GET: RequestHandler = async ({ url, cookies }) => {
	const cfg = getConfig();
	if (!cfg) throw error(404, 'authentication is not configured');

	const rawTx = cookies.get(TX_COOKIE);
	const tx = rawTx ? await openTransaction(rawTx, cfg.sessionSecret) : null;
	cookies.delete(TX_COOKIE, { path: '/' });

	// Missing/expired handshake (e.g. user bookmarked the callback) → restart.
	if (!tx) {
		redirect(302, '/auth/login');
	}

	try {
		const session = await completeLogin(cfg, url, {
			state: tx.state,
			nonce: tx.nonce,
			codeVerifier: tx.codeVerifier
		});

		setSessionCookie(cookies, await sealSession(session, cfg.sessionSecret, cfg.sessionTtl), {
			path: '/',
			httpOnly: true,
			secure: !cfg.allowInsecure,
			sameSite: 'lax',
			maxAge: cfg.sessionTtl
		});
	} catch {
		// Bad state/nonce/code, or token exchange failure → start over.
		redirect(302, '/auth/login');
	}

	redirect(302, tx.returnTo || '/');
};
