// Server-only session handling. The session (the API access token plus the
// signed-in user's identity) is sealed into an encrypted JWE and stored in an
// HttpOnly cookie, so the browser never sees a token in JS.
// AES-GCM (dir/A256GCM) gives confidentiality + integrity in one primitive.
//
// This module is pure (crypto + encoding) so it is unit-testable without a
// running API.
import type { Cookies } from '@sveltejs/kit';
import { EncryptJWT, jwtDecrypt, base64url } from 'jose';
import { createHash } from 'node:crypto';

/** Cookie holding the sealed session for an authenticated user. */
export const SESSION_COOKIE = 'lkb_session';

/** The authenticated session persisted (encrypted) across requests. */
export interface Session {
	/** The API-issued access token, forwarded as a Bearer token server-side. */
	accessToken: string;
	/** Absolute access-token expiry, epoch milliseconds. */
	expiresAt: number;
	userId: string;
	email: string;
}

const ENC = 'A256GCM';
const ALG = 'dir';

// Derive a stable 32-byte key from the configured secret. Hashing lets operators
// use any sufficiently strong secret string without worrying about exact length.
function keyFrom(secret: string): Uint8Array {
	if (!secret || secret.length < 16) {
		throw new Error('SESSION_COOKIE_SECRET must be set and at least 16 characters');
	}
	return new Uint8Array(createHash('sha256').update(secret).digest());
}

/** seal encrypts an arbitrary JSON-serializable payload into a compact JWE. */
async function seal(
	payload: Record<string, unknown>,
	secret: string,
	ttlSeconds: number
): Promise<string> {
	return new EncryptJWT(payload)
		.setProtectedHeader({ alg: ALG, enc: ENC })
		.setIssuedAt()
		.setExpirationTime(`${ttlSeconds}s`)
		.encrypt(keyFrom(secret));
}

/** open decrypts and validates a compact JWE, returning null on any failure. */
async function open<T>(token: string, secret: string): Promise<T | null> {
	try {
		const { payload } = await jwtDecrypt(token, keyFrom(secret));
		return payload as unknown as T;
	} catch {
		return null;
	}
}

export function sealSession(session: Session, secret: string, ttlSeconds: number): Promise<string> {
	return seal({ ...session }, secret, ttlSeconds);
}

export function openSession(token: string, secret: string): Promise<Session | null> {
	return open<Session>(token, secret);
}

/** isExpired reports whether the access token is at/past its expiry (with a small skew). */
export function isExpired(session: Session, skewSeconds = 30): boolean {
	return Date.now() >= session.expiresAt - skewSeconds * 1000;
}

// ── Session cookie chunking ──────────────────────────────────────────────────
// A sealed session can approach the browser's ~4096-byte per-cookie limit (the
// browser then silently drops it and the user loops through login forever). So the
// sealed value is split across numbered cookies (`lkb_session.0`, `.1`, …), each
// safely under the limit, and rejoined on read. Callers use these helpers instead
// of touching SESSION_COOKIE directly.

/** Max chars per cookie value; well under 4096 once the name + attributes are added. */
const MAX_COOKIE_VALUE = 3800;
/** Upper bound on chunks read/cleared — generous headroom over realistic token sizes. */
const MAX_SESSION_CHUNKS = 8;

type SessionCookieOptions = Parameters<Cookies['set']>[2];

/** setSessionCookie writes the sealed session across as many chunk cookies as needed. */
export function setSessionCookie(
	cookies: Cookies,
	sealed: string,
	opts: SessionCookieOptions
): void {
	const parts: string[] = [];
	for (let i = 0; i < sealed.length; i += MAX_COOKIE_VALUE) {
		parts.push(sealed.slice(i, i + MAX_COOKIE_VALUE));
	}
	parts.forEach((part, i) => cookies.set(`${SESSION_COOKIE}.${i}`, part, opts));
	// Drop leftover chunks (and any legacy single cookie) from a larger prior session.
	clearStaleSessionCookies(cookies, parts.length, opts.path);
}

/** getSessionCookie rejoins the chunk cookies (falling back to a legacy single cookie). */
export function getSessionCookie(cookies: Cookies): string | null {
	if (cookies.get(`${SESSION_COOKIE}.0`) === undefined) {
		return cookies.get(SESSION_COOKIE) ?? null;
	}
	let sealed = '';
	for (let i = 0; i < MAX_SESSION_CHUNKS; i++) {
		const part = cookies.get(`${SESSION_COOKIE}.${i}`);
		if (part === undefined) break;
		sealed += part;
	}
	return sealed;
}

/** clearSessionCookie removes every session chunk (and the legacy single cookie). */
export function clearSessionCookie(cookies: Cookies, path = '/'): void {
	clearStaleSessionCookies(cookies, 0, path);
}

function clearStaleSessionCookies(cookies: Cookies, keep: number, path = '/'): void {
	if (cookies.get(SESSION_COOKIE) !== undefined) cookies.delete(SESSION_COOKIE, { path });
	for (let i = keep; i < MAX_SESSION_CHUNKS; i++) {
		if (cookies.get(`${SESSION_COOKIE}.${i}`) !== undefined) {
			cookies.delete(`${SESSION_COOKIE}.${i}`, { path });
		}
	}
}

// Re-exported so callers importing this module don't also need jose directly.
export { base64url };
