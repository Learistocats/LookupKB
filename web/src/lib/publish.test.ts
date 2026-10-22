import { describe, expect, it, vi } from 'vitest';
import { publishDraft, unpublishDraft, accessLine, type FieldError } from './publish';

function jsonResponse(body: unknown, status = 200): Response {
	return new Response(JSON.stringify(body), {
		status,
		headers: { 'content-type': 'application/json' }
	});
}

describe('publishDraft', () => {
	it('posts the metadata to the same-origin proxy and returns the published doc', async () => {
		const doc = { id: 'd1', slug: 'a-title-abcd', status: 'published' };
		const fetchFn = vi.fn().mockResolvedValue(jsonResponse(doc));

		const out = await publishDraft(
			'd1',
			{ title: 'A title', description: 'A long enough description.', tags: ['ops'] },
			fetchFn as unknown as typeof fetch
		);

		expect(out).toEqual({ ok: true, doc });
		expect(fetchFn).toHaveBeenCalledWith(
			'/entries/d1/publish',
			expect.objectContaining({ method: 'POST' })
		);
		// The body carries exactly the confirm-flow metadata.
		const init = fetchFn.mock.calls[0][1] as RequestInit;
		expect(JSON.parse(init.body as string)).toEqual({
			title: 'A title',
			description: 'A long enough description.',
			tags: ['ops']
		});
	});

	it('returns per-field errors on a 422 without throwing (document stays draft)', async () => {
		const errors: FieldError[] = [
			{ field: 'title', message: 'title is required' },
			{ field: 'tags', message: 'at least 1 tag(s) required' }
		];
		const fetchFn = vi.fn().mockResolvedValue(jsonResponse({ errors }, 422));

		const out = await publishDraft(
			'd1',
			{ title: '', description: '', tags: [] },
			fetchFn as unknown as typeof fetch
		);

		expect(out).toEqual({ ok: false, fieldErrors: errors });
	});

	it('throws on other non-OK responses (404/5xx)', async () => {
		const fetchFn = vi.fn().mockResolvedValue(new Response('nope', { status: 404 }));
		await expect(
			publishDraft(
				'd1',
				{ title: 't', description: 'd', tags: ['x'] },
				fetchFn as unknown as typeof fetch
			)
		).rejects.toThrow('404');
	});
});

describe('unpublishDraft', () => {
	it('posts to the unpublish proxy and returns the reverted doc', async () => {
		const doc = { id: 'd1', slug: 'a-title-abcd', status: 'unpublished' };
		const fetchFn = vi.fn().mockResolvedValue(jsonResponse(doc));

		const got = await unpublishDraft('d1', fetchFn as unknown as typeof fetch);
		expect(got).toEqual(doc);
		expect(fetchFn).toHaveBeenCalledWith('/entries/d1/unpublish', { method: 'POST' });
	});

	it('throws on a non-OK response', async () => {
		const fetchFn = vi.fn().mockResolvedValue(new Response('no', { status: 502 }));
		await expect(unpublishDraft('d1', fetchFn as unknown as typeof fetch)).rejects.toThrow('502');
	});
});

describe('accessLine', () => {
	it('says everyone signed in when no selected tag is protected', () => {
		expect(accessLine([])).toBe('Visible to everyone signed in to this knowledge base.');
	});

	it('names the protected tags otherwise', () => {
		expect(accessLine(['reeflect'])).toBe(
			'Only members of groups with access to reeflect (and you) can see this entry.'
		);
		expect(accessLine(['hr', 'reeflect'])).toBe(
			'Only members of groups with access to hr, reeflect (and you) can see this entry.'
		);
	});
});

describe('publishDraft protected-tag refusal', () => {
	it('returns the 422 tags error for the dialog to show on the tags field', async () => {
		const errors = [
			{ field: 'tags', message: `tag "reeflect" is protected; you don't have access to it` }
		];
		const fetchFn = vi.fn().mockResolvedValue(jsonResponse({ errors }, 422));
		const out = await publishDraft(
			'd1',
			{ title: 't', description: 'd', tags: ['reeflect'] },
			fetchFn as unknown as typeof fetch
		);
		expect(out).toEqual({ ok: false, fieldErrors: errors });
	});
});
