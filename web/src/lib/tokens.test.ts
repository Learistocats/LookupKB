import { describe, expect, it } from 'vitest';
import { maskedToken, parseCreateTokenForm, tokenStatus, type ApiToken } from './tokens';

describe('parseCreateTokenForm', () => {
	it('trims the name and omits expiry when "never"', () => {
		expect(parseCreateTokenForm('  ci  ', '')).toEqual({ ok: true, body: { name: 'ci' } });
	});

	it('carries a valid expiry as an integer', () => {
		expect(parseCreateTokenForm('ci', '30')).toEqual({
			ok: true,
			body: { name: 'ci', expires_in_days: 30 }
		});
	});

	it('rejects a missing or overlong name', () => {
		expect(parseCreateTokenForm('   ', '').ok).toBe(false);
		expect(parseCreateTokenForm(null, '').ok).toBe(false);
		expect(parseCreateTokenForm('x'.repeat(101), '').ok).toBe(false);
	});

	it('rejects a bogus expiry', () => {
		for (const e of ['0', '366', '1.5', 'abc', '-3']) {
			expect(parseCreateTokenForm('ci', e).ok).toBe(false);
		}
	});
});

describe('tokenStatus', () => {
	const base: ApiToken = {
		id: '1',
		name: 'ci',
		prefix: 'abcdefgh',
		created_at: '2026-01-01T00:00:00Z',
		last_used_at: null,
		expires_at: null,
		revoked_at: null
	};
	const now = Date.parse('2026-06-01T00:00:00Z');

	it('is active without expiry or revocation', () => {
		expect(tokenStatus(base, now)).toBe('active');
		expect(tokenStatus({ ...base, expires_at: '2027-01-01T00:00:00Z' }, now)).toBe('active');
	});

	it('is expired past its expiry', () => {
		expect(tokenStatus({ ...base, expires_at: '2026-05-01T00:00:00Z' }, now)).toBe('expired');
	});

	it('revocation wins', () => {
		expect(
			tokenStatus(
				{ ...base, revoked_at: '2026-02-01T00:00:00Z', expires_at: '2026-05-01T00:00:00Z' },
				now
			)
		).toBe('revoked');
	});
});

describe('maskedToken', () => {
	it('shows only the prefix', () => {
		expect(maskedToken('abcdefgh')).toBe('lkb_pat_abcdefgh_…');
	});
});
