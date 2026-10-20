// Personal Access Token settings logic (LOO-42). Pure so the settings page's
// form parsing and status rules are unit-tested; the API is authoritative and
// re-validates everything.

/** A token as the API lists it (never the secret or its hash). */
export interface ApiToken {
	id: string;
	name: string;
	prefix: string;
	created_at: string;
	last_used_at: string | null;
	expires_at: string | null;
	revoked_at: string | null;
}

/** The creation response: the listed fields plus the plaintext, shown once. */
export interface CreatedToken extends ApiToken {
	token: string;
}

export const TOKEN_NAME_MAX = 100;

/** Expiry choices offered in the UI ('' = never expires). */
export const EXPIRY_OPTIONS: { value: string; label: string }[] = [
	{ value: '7', label: '7 days' },
	{ value: '30', label: '30 days' },
	{ value: '90', label: '90 days' },
	{ value: '365', label: '1 year' },
	{ value: '', label: 'No expiry' }
];

/** The expiry preselected in the create form. */
export const DEFAULT_EXPIRY = '90';

export interface CreateTokenBody {
	name: string;
	expires_in_days?: number;
}

export type ParsedCreate = { ok: true; body: CreateTokenBody } | { ok: false; error: string };

/** Validate the create form into the API request body. */
export function parseCreateTokenForm(name: unknown, expiry: unknown): ParsedCreate {
	const n = typeof name === 'string' ? name.trim() : '';
	if (!n) return { ok: false, error: 'Give the token a name.' };
	if ([...n].length > TOKEN_NAME_MAX) {
		return { ok: false, error: `Keep the name under ${TOKEN_NAME_MAX} characters.` };
	}
	const e = typeof expiry === 'string' ? expiry.trim() : '';
	if (e === '') return { ok: true, body: { name: n } };
	const days = Number(e);
	if (!Number.isInteger(days) || days < 1 || days > 365) {
		return { ok: false, error: 'Pick a valid expiry.' };
	}
	return { ok: true, body: { name: n, expires_in_days: days } };
}

export type TokenStatus = 'active' | 'expired' | 'revoked';

export function tokenStatus(t: ApiToken, now: number = Date.now()): TokenStatus {
	if (t.revoked_at) return 'revoked';
	if (t.expires_at && Date.parse(t.expires_at) <= now) return 'expired';
	return 'active';
}

/** The display form of a token: its non-secret prefix with the secret elided. */
export function maskedToken(prefix: string): string {
	return `lkb_pat_${prefix}_…`;
}
