// Begins the OIDC Authorization Code + PKCE flow: stash the handshake state in a
// short-lived encrypted cookie and redirect the browser to Authentik (AC-1.1).
import { redirect, error, type RequestHandler } from '@sveltejs/kit';
import { getConfig, startLogin } from '$lib/server/oidc';
import { TX_COOKIE, sealTransaction } from '$lib/server/session';

// The handshake must complete quickly; the tx cookie lives only a few minutes.
const TX_TTL_SECONDS = 300;

// Only allow same-origin relative return paths to avoid open-redirect abuse.
function safeReturnTo(raw: string | null): string {
	if (raw && raw.startsWith('/') && !raw.startsWith('//')) return raw;
	return '/';
}

export const GET: RequestHandler = async ({ url, cookies }) => {
	const cfg = getConfig();
	if (!cfg) throw error(404, 'authentication is not configured');

	const returnTo = safeReturnTo(url.searchParams.get('returnTo'));
	const { authorizationUrl, state, nonce, codeVerifier } = await startLogin(cfg);

	cookies.set(
		TX_COOKIE,
		await sealTransaction(
			{ state, nonce, codeVerifier, returnTo },
			cfg.sessionSecret,
			TX_TTL_SECONDS
		),
		{
			path: '/',
			httpOnly: true,
			secure: !cfg.allowInsecure,
			sameSite: 'lax',
			maxAge: TX_TTL_SECONDS
		}
	);

	redirect(302, authorizationUrl);
};
