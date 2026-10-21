import { describe, expect, it, vi } from 'vitest';
import { AdminApiError, grantTag, protectPrompt, revokeTag, unprotectPrompt } from './admin';

function jsonResponse(body: unknown, status = 200): Response {
	return new Response(JSON.stringify(body), {
		status,
		headers: { 'content-type': 'application/json' }
	});
}

const needsConfirm = (tag: string) => jsonResponse({ error: 'needs_confirmation', tag }, 409);
const grant = { tag_id: 't1', slug: 'secret', label: 'secret' };

describe('prompts', () => {
	it('use the exact product wording', () => {
		expect(protectPrompt('secret')).toBe(
			'This action will change the tag "secret" to a protected tag, are you sure?'
		);
		expect(unprotectPrompt('secret')).toBe(
			'This will make the tag "secret" public again, are you sure?'
		);
	});
});

describe('grantTag', () => {
	it('grants directly when the tag is already protected (no prompt)', async () => {
		const fetchFn = vi.fn().mockResolvedValue(jsonResponse(grant, 201));
		const ask = vi.fn();

		const out = await grantTag('g1', 'secret', ask, fetchFn as unknown as typeof fetch);

		expect(out).toEqual({ status: 'done', value: grant });
		expect(ask).not.toHaveBeenCalled();
		expect(fetchFn).toHaveBeenCalledTimes(1);
		const [url, init] = fetchFn.mock.calls[0] as [string, RequestInit];
		expect(url).toBe('/admin/groups/g1/tags');
		expect(JSON.parse(init.body as string)).toEqual({ tag: 'secret', confirm_protect: false });
	});

	it('asks on 409 needs_confirmation and retries with confirm_protect', async () => {
		const fetchFn = vi
			.fn()
			.mockResolvedValueOnce(needsConfirm('secret'))
			.mockResolvedValueOnce(jsonResponse(grant, 201));
		const ask = vi.fn().mockResolvedValue(true);

		const out = await grantTag('g1', 'Secret', ask, fetchFn as unknown as typeof fetch);

		expect(out).toEqual({ status: 'done', value: grant });
		expect(ask).toHaveBeenCalledWith(
			'This action will change the tag "secret" to a protected tag, are you sure?'
		);
		const retry = fetchFn.mock.calls[1][1] as RequestInit;
		expect(JSON.parse(retry.body as string)).toEqual({ tag: 'Secret', confirm_protect: true });
	});

	it('stops without a second request when the admin cancels', async () => {
		const fetchFn = vi.fn().mockResolvedValue(needsConfirm('secret'));
		const out = await grantTag(
			'g1',
			'secret',
			vi.fn().mockResolvedValue(false),
			fetchFn as unknown as typeof fetch
		);
		expect(out).toEqual({ status: 'cancelled' });
		expect(fetchFn).toHaveBeenCalledTimes(1);
	});

	it('throws the API error message on other failures', async () => {
		const fetchFn = vi.fn().mockResolvedValue(jsonResponse({ error: 'tag is required' }, 422));
		await expect(
			grantTag('g1', '', vi.fn(), fetchFn as unknown as typeof fetch)
		).rejects.toMatchObject({ status: 422, message: 'tag is required' });
	});

	it('treats a non-confirmation 409 as an error, not a prompt', async () => {
		const fetchFn = vi.fn().mockResolvedValue(jsonResponse({ error: 'conflict' }, 409));
		const ask = vi.fn();
		await expect(
			grantTag('g1', 'x', ask, fetchFn as unknown as typeof fetch)
		).rejects.toBeInstanceOf(AdminApiError);
		expect(ask).not.toHaveBeenCalled();
	});
});

describe('revokeTag', () => {
	it('revokes directly when other groups still grant the tag', async () => {
		const fetchFn = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
		const out = await revokeTag('g1', 't1', vi.fn(), fetchFn as unknown as typeof fetch);
		expect(out).toEqual({ status: 'done', value: null });
		expect(fetchFn).toHaveBeenCalledWith('/admin/groups/g1/tags/t1', { method: 'DELETE' });
	});

	it('asks before removing the last grant and retries with confirm_unprotect', async () => {
		const fetchFn = vi
			.fn()
			.mockResolvedValueOnce(needsConfirm('secret'))
			.mockResolvedValueOnce(new Response(null, { status: 204 }));
		const ask = vi.fn().mockResolvedValue(true);

		const out = await revokeTag('g1', 't1', ask, fetchFn as unknown as typeof fetch);

		expect(out.status).toBe('done');
		expect(ask).toHaveBeenCalledWith('This will make the tag "secret" public again, are you sure?');
		expect(fetchFn.mock.calls[1][0]).toBe('/admin/groups/g1/tags/t1?confirm_unprotect=true');
	});

	it('returns cancelled when the admin declines', async () => {
		const fetchFn = vi.fn().mockResolvedValue(needsConfirm('secret'));
		const out = await revokeTag(
			'g1',
			't1',
			vi.fn().mockResolvedValue(false),
			fetchFn as unknown as typeof fetch
		);
		expect(out).toEqual({ status: 'cancelled' });
		expect(fetchFn).toHaveBeenCalledTimes(1);
	});
});
