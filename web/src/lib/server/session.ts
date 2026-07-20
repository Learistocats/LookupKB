// Server-only session handling. The session (access + refresh tokens plus the
// projected identity) is sealed into an encrypted JWE and stored in an HttpOnly
// cookie, so the browser never sees a token in JS (06-security.md §5). AES-GCM
// (dir/A256GCM) gives confidentiality + integrity in one primitive.
//
// This module is pure (crypto + encoding) so it is unit-testable without a
// running Authentik.
import { EncryptJWT, jwtDecrypt, base64url } from 'jose';
import { createHash } from 'node:crypto';

/** Cookie holding the sealed session for an authenticated user. */
export const SESSION_COOKIE = 'lkb_session';

/** Short-lived cookie holding the in-flight OIDC handshake (PKCE/state/nonce). */
export const TX_COOKIE = 'lkb_oidc_tx';

/** The authenticated session persisted (encrypted) across requests. */
export interface Session {
	accessToken: string;
	refreshToken?: string;
	/** Kept only as a logout (RP-initiated end-session) hint. */
	idToken?: string;
	/** Absolute access-token expiry, epoch milliseconds. */
	expiresAt: number;
	sub: string;
	email: string;
	groups: string[];
}

/** In-flight handshake state stashed between /auth/login and /auth/callback. */
export interface OidcTransaction {
	state: string;
	nonce: string;
	codeVerifier: string;
	/** Where to send the user once login completes. */
	returnTo: string;
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

export function sealTransaction(
	tx: OidcTransaction,
	secret: string,
	ttlSeconds: number
): Promise<string> {
	return seal({ ...tx }, secret, ttlSeconds);
}

export function openTransaction(token: string, secret: string): Promise<OidcTransaction | null> {
	return open<OidcTransaction>(token, secret);
}

/** isExpired reports whether the access token is at/past its expiry (with a small skew). */
export function isExpired(session: Session, skewSeconds = 30): boolean {
	return Date.now() >= session.expiresAt - skewSeconds * 1000;
}

// Re-exported so callers importing this module don't also need jose directly.
export { base64url };
