import { describe, expect, it, vi } from 'vitest';
import { canDeleteEntry, deleteEntry } from './drafts';

describe('canDeleteEntry', () => {
	it('allows persisted drafts and unpublished entries', () => {
		expect(canDeleteEntry('d1', 'draft')).toBe(true);
		expect(canDeleteEntry('d1', 'unpublished')).toBe(true);
	});

	it('refuses published entries and entries with no row yet', () => {
		expect(canDeleteEntry('d1', 'published')).toBe(false);
		expect(canDeleteEntry(null, 'draft')).toBe(false);
		expect(canDeleteEntry('new', 'draft')).toBe(false);
	});
});

describe('deleteEntry', () => {
	it('sends DELETE to the same-origin proxy', async () => {
		const fetchFn = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));

		await deleteEntry('d1', fetchFn as unknown as typeof fetch);

		expect(fetchFn).toHaveBeenCalledWith('/entries/d1/delete', { method: 'DELETE' });
	});

	it('throws a readable error on 404', async () => {
		const fetchFn = vi.fn().mockResolvedValue(new Response('nope', { status: 404 }));
		await expect(deleteEntry('d1', fetchFn as unknown as typeof fetch)).rejects.toThrow(
			/no longer exists/
		);
	});

	it('throws on other non-OK responses', async () => {
		const fetchFn = vi.fn().mockResolvedValue(new Response('bad', { status: 502 }));
		await expect(deleteEntry('d1', fetchFn as unknown as typeof fetch)).rejects.toThrow('502');
	});
});
