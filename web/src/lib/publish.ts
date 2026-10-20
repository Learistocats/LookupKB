// Publish-flow client logic (F5, LOO-14). The publish confirm dialog collects the
// metadata; this module builds the request, calls the same-origin proxy (which
// attaches the bearer server-side), and normalizes the outcome — success, or the
// server's per-field validation errors (422). Kept out of the component so it is
// unit-testable and the dialog stays presentational.
//
// Client-side hints (validateTags, length checks) are UX only; the SERVER is always
// authoritative and re-validates everything (06-security.md §6).

/** A single per-field validation error, matching the Go handler's 422 body. */
export interface FieldError {
	field: string;
	message: string;
}

/**
 * The metadata a publish attempt carries. There is no access field: who can read
 * the entry follows from its tags and the group tag policy (LOO-33).
 */
export interface PublishRequest {
	title: string;
	description: string;
	tags: string[];
}

/** The published/unpublished document projection the API returns on success. */
export interface PublishedDoc {
	id: string;
	slug: string;
	status: string;
	title: string | null;
	description: string | null;
	published_at: string;
	updated_at: string;
}

/**
 * PublishOutcome is a discriminated result: `ok` with the published doc, or
 * `fieldErrors` when the server's metadata gate rejected the attempt (422). Any
 * other failure (network, 404, 5xx) throws.
 */
export type PublishOutcome =
	{ ok: true; doc: PublishedDoc } | { ok: false; fieldErrors: FieldError[] };

/**
 * publishDraft posts the confirm-flow metadata to the same-origin proxy at
 * /entries/{id}/publish (NOT /api/*, to avoid the dev-time Vite /api proxy). On 422
 * it returns the per-field errors so the dialog can render them inline and keep the
 * document a draft; on any other non-OK status it throws.
 */
export async function publishDraft(
	id: string,
	req: PublishRequest,
	fetchFn: typeof fetch = fetch
): Promise<PublishOutcome> {
	const res = await fetchFn(`/entries/${id}/publish`, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(req)
	});

	if (res.status === 422) {
		const body = (await res.json()) as { errors?: FieldError[] };
		return { ok: false, fieldErrors: body.errors ?? [] };
	}
	if (!res.ok) throw new Error(`publish failed (${res.status})`);

	return { ok: true, doc: (await res.json()) as PublishedDoc };
}

/**
 * unpublishDraft posts to /entries/{id}/unpublish and returns the reverted document.
 * Throws on any non-OK status (there is no per-field failure mode for unpublish).
 */
export async function unpublishDraft(
	id: string,
	fetchFn: typeof fetch = fetch
): Promise<PublishedDoc> {
	const res = await fetchFn(`/entries/${id}/unpublish`, { method: 'POST' });
	if (!res.ok) throw new Error(`unpublish failed (${res.status})`);
	return (await res.json()) as PublishedDoc;
}

/** groupErrorsByField buckets a flat FieldError list by field for inline display. */
export function groupErrorsByField(errors: FieldError[]): Record<string, string[]> {
	const out: Record<string, string[]> = {};
	for (const e of errors) {
		(out[e.field] ??= []).push(e.message);
	}
	return out;
}

/**
 * accessLine is the publish dialog's "who can see this" sentence (LOO-33): every
 * signed-in user when no selected tag is protected, otherwise only members of
 * groups granted those tags — plus the author, who always reads their own entry.
 */
export function accessLine(protectedTags: string[]): string {
	if (protectedTags.length === 0) return 'Visible to everyone signed in to this knowledge base.';
	return `Only members of groups with access to ${protectedTags.join(', ')} (and you) can see this entry.`;
}
