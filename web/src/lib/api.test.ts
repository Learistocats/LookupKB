import { describe, expect, it, vi } from 'vitest';
import { getHealth } from './api';

describe('getHealth', () => {
	it('parses a healthy response', async () => {
		const fetchFn = vi.fn().mockResolvedValue(
			new Response(JSON.stringify({ status: 'ok', service: 'lookupkb-api', version: 'test' }), {
				status: 200,
				headers: { 'content-type': 'application/json' }
			})
		);

		const health = await getHealth(fetchFn as unknown as typeof fetch);
		expect(health.status).toBe('ok');
		expect(fetchFn).toHaveBeenCalledWith('/healthz');
	});

	it('throws on a non-200 response', async () => {
		const fetchFn = vi.fn().mockResolvedValue(new Response('nope', { status: 503 }));
		await expect(getHealth(fetchFn as unknown as typeof fetch)).rejects.toThrow('503');
	});
});
