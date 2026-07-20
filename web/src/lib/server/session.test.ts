import { describe, it, expect } from 'vitest';
import {
	sealSession,
	openSession,
	sealTransaction,
	openTransaction,
	isExpired,
	type Session,
	type OidcTransaction
} from './session';

const secret = 'test-secret-at-least-16-chars-long';

function sampleSession(overrides: Partial<Session> = {}): Session {
	return {
		accessToken: 'access-token-abc',
		refreshToken: 'refresh-token-xyz',
		idToken: 'id-token-123',
		expiresAt: Date.now() + 3600_000,
		sub: 'ak-sub-1',
		email: 'user@example.com',
		groups: ['security', 'hr'],
		...overrides
	};
}

describe('session sealing', () => {
	it('round-trips a session through encryption', async () => {
		const session = sampleSession();
		const sealed = await sealSession(session, secret, 3600);
		expect(sealed.split('.')).toHaveLength(5); // compact JWE has 5 segments

		const opened = await openSession(sealed, secret);
		expect(opened).not.toBeNull();
		expect(opened?.accessToken).toBe(session.accessToken);
		expect(opened?.groups).toEqual(['security', 'hr']);
		expect(opened?.sub).toBe('ak-sub-1');
	});

	it('does not leak plaintext token material into the cookie', async () => {
		const sealed = await sealSession(sampleSession(), secret, 3600);
		expect(sealed).not.toContain('access-token-abc');
		expect(sealed).not.toContain('refresh-token-xyz');
	});

	it('rejects a token sealed with a different secret', async () => {
		const sealed = await sealSession(sampleSession(), secret, 3600);
		const opened = await openSession(sealed, 'a-totally-different-secret-value');
		expect(opened).toBeNull();
	});

	it('rejects a tampered token', async () => {
		const sealed = await sealSession(sampleSession(), secret, 3600);
		const tampered = sealed.slice(0, -3) + 'AAA';
		const opened = await openSession(tampered, secret);
		expect(opened).toBeNull();
	});

	it('rejects an expired sealed session', async () => {
		const sealed = await sealSession(sampleSession(), secret, 1);
		await new Promise((r) => setTimeout(r, 1100));
		const opened = await openSession(sealed, secret);
		expect(opened).toBeNull();
	});

	it('round-trips an OIDC handshake transaction', async () => {
		const tx: OidcTransaction = {
			state: 'state-1',
			nonce: 'nonce-1',
			codeVerifier: 'verifier-1',
			returnTo: '/documents/foo'
		};
		const sealed = await sealTransaction(tx, secret, 300);
		const opened = await openTransaction(sealed, secret);
		expect(opened?.state).toBe('state-1');
		expect(opened?.returnTo).toBe('/documents/foo');
	});

	it('requires a sufficiently long secret', async () => {
		await expect(sealSession(sampleSession(), 'short', 3600)).rejects.toThrow(/at least 16/);
	});
});

describe('isExpired', () => {
	it('is false well before expiry', () => {
		expect(isExpired(sampleSession({ expiresAt: Date.now() + 3600_000 }))).toBe(false);
	});

	it('is true past expiry', () => {
		expect(isExpired(sampleSession({ expiresAt: Date.now() - 1000 }))).toBe(true);
	});

	it('honours the skew window (treats near-expiry as expired)', () => {
		expect(isExpired(sampleSession({ expiresAt: Date.now() + 10_000 }), 30)).toBe(true);
	});
});
