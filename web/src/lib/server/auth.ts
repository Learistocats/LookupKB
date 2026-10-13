// Server-only auth configuration and helpers for the email/password login flow.
// The Go API owns credentials and issues the access token; this BFF only seals
// that token into the session cookie and forwards it server-side.
import type { Cookies } from '@sveltejs/kit';
import { env } from '$env/dynamic/private';
import { apiFetch } from './api';
import { sealSession, setSessionCookie, type Session } from './session';

export interface AuthConfig {
	sessionSecret: string;
	/** Mark cookies Secure. Disable only for plain-http local development. */
	secure: boolean;
}

/**
 * getAuthConfig returns the auth configuration, or null when no session secret
 * is configured — mirroring the API's "empty secret disables auth" so the web app
 * still boots for `pnpm dev` without a backend.
 */
export function getAuthConfig(): AuthConfig | null {
	const sessionSecret = env.SESSION_COOKIE_SECRET?.trim();
	if (!sessionSecret) return null;
	return { sessionSecret, secure: env.SESSION_COOKIE_INSECURE !== 'true' };
}

/** Only same-origin relative return paths are honored (no open redirect). */
export function safeReturnTo(raw: string | null | undefined): string {
	if (raw && raw.startsWith('/') && !raw.startsWith('//') && !raw.startsWith('/\\')) return raw;
	return '/';
}

/** Shape of the API's signup/login success body. */
interface SessionResponse {
	token: string;
	expires_at: string;
	user: { id: string; email: string; display_name: string | null };
}

export type CredentialResult = { ok: true } | { ok: false; status: number; message: string };

/**
 * exchangeCredentials posts email/password to the API (login or signup) and, on
 * success, seals the returned token into the session cookie. Errors carry the
 * API's message (already generic for login failures) for the form to show.
 */
export async function exchangeCredentials(
	cfg: AuthConfig,
	cookies: Cookies,
	kind: 'login' | 'signup',
	body: { email: string; password: string; display_name?: string },
	clientAddress: string
): Promise<CredentialResult> {
	let res: Response;
	try {
		res = await apiFetch(undefined, `/api/v1/auth/${kind}`, {
			method: 'POST',
			headers: { 'content-type': 'application/json', 'x-forwarded-for': clientAddress },
			body: JSON.stringify(body)
		});
	} catch {
		return { ok: false, status: 502, message: 'The server is unreachable. Try again shortly.' };
	}

	if (!res.ok) {
		const data = (await res.json().catch(() => ({}))) as { error?: string };
		return { ok: false, status: res.status, message: data.error ?? 'Something went wrong.' };
	}

	const data = (await res.json()) as SessionResponse;
	const expiresAt = Date.parse(data.expires_at);
	const ttl = Math.max(60, Math.floor((expiresAt - Date.now()) / 1000));
	const session: Session = {
		accessToken: data.token,
		expiresAt,
		userId: data.user.id,
		email: data.user.email
	};
	setSessionCookie(cookies, await sealSession(session, cfg.sessionSecret, ttl), {
		path: '/',
		httpOnly: true,
		secure: cfg.secure,
		sameSite: 'lax',
		maxAge: ttl
	});
	return { ok: true };
}
