// Super-admin tag-policy client logic (LOO-56, F10). Granting a group a tag that
// no group holds yet turns it into a *protected* tag; removing the last grant makes
// it public again. The API refuses both with 409 {"error":"needs_confirmation"}
// unless the caller confirms, so this module: calls the same-origin proxy without
// confirmation, asks the admin on a 409, and retries with the confirm flag.
//
// Pure (fetchFn and the prompt are injected) so it is unit-testable; the page only
// supplies a dialog-backed `ask`. The API is authoritative for every rule here.

/** A tag in a group's policy, as the API returns it. */
export interface TagGrant {
	tag_id: string;
	slug: string;
	label: string;
	created_at?: string;
}

/** The exact prompt shown before a tag becomes protected. */
export function protectPrompt(tag: string): string {
	return `This action will change the tag "${tag}" to a protected tag, are you sure?`;
}

/** The exact prompt shown before a tag's last grant is removed. */
export function unprotectPrompt(tag: string): string {
	return `This will make the tag "${tag}" public again, are you sure?`;
}

/** ask shows a confirmation and resolves true when the admin accepts. */
export type Ask = (message: string) => Promise<boolean>;

/** The outcome of a policy change: applied, or cancelled at the confirm prompt. */
export type PolicyOutcome<T> = { status: 'done'; value: T } | { status: 'cancelled' };

/** AdminApiError carries the API's status and its `error` message. */
export class AdminApiError extends Error {
	constructor(
		readonly status: number,
		message: string
	) {
		super(message);
		this.name = 'AdminApiError';
	}
}

async function errorFrom(res: Response): Promise<AdminApiError> {
	let message = `request failed (${res.status})`;
	try {
		const body = (await res.json()) as { error?: string };
		if (body.error) message = body.error;
	} catch {
		// non-JSON body: keep the generic message
	}
	return new AdminApiError(res.status, message);
}

/**
 * needsConfirmation reads a 409 body and returns the tag label when the API asks
 * for confirmation, or null for any other 409 (e.g. a name conflict).
 */
async function needsConfirmation(res: Response): Promise<string | null> {
	if (res.status !== 409) return null;
	try {
		const body = (await res.clone().json()) as { error?: string; tag?: string };
		return body.error === 'needs_confirmation' ? (body.tag ?? '') : null;
	} catch {
		return null;
	}
}

/**
 * withConfirmation runs `attempt(false)`; when the API answers 409
 * needs_confirmation it asks with `prompt(tag)` and, if accepted, runs
 * `attempt(true)` once. Any other non-OK response throws AdminApiError.
 */
export async function withConfirmation(
	attempt: (confirm: boolean) => Promise<Response>,
	prompt: (tag: string) => string,
	ask: Ask
): Promise<PolicyOutcome<Response>> {
	let res = await attempt(false);
	const tag = await needsConfirmation(res);
	if (tag !== null) {
		if (!(await ask(prompt(tag)))) return { status: 'cancelled' };
		res = await attempt(true);
	}
	if (!res.ok) throw await errorFrom(res);
	return { status: 'done', value: res };
}

/** grantTag adds `tag` to the group's policy, confirming first-grant protection. */
export async function grantTag(
	groupId: string,
	tag: string,
	ask: Ask,
	fetchFn: typeof fetch = fetch
): Promise<PolicyOutcome<TagGrant>> {
	const out = await withConfirmation(
		(confirm) =>
			fetchFn(`/admin/groups/${encodeURIComponent(groupId)}/tags`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ tag, confirm_protect: confirm })
			}),
		protectPrompt,
		ask
	);
	if (out.status === 'cancelled') return out;
	return { status: 'done', value: (await out.value.json()) as TagGrant };
}

/** revokeTag removes a tag from the group's policy, confirming last-grant removal. */
export async function revokeTag(
	groupId: string,
	tagId: string,
	ask: Ask,
	fetchFn: typeof fetch = fetch
): Promise<PolicyOutcome<null>> {
	const base = `/admin/groups/${encodeURIComponent(groupId)}/tags/${encodeURIComponent(tagId)}`;
	const out = await withConfirmation(
		(confirm) => fetchFn(confirm ? `${base}?confirm_unprotect=true` : base, { method: 'DELETE' }),
		unprotectPrompt,
		ask
	);
	if (out.status === 'cancelled') return out;
	return { status: 'done', value: null };
}
