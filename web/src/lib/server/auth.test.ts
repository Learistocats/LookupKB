import { describe, it, expect } from 'vitest';
import { safeReturnTo } from './auth';

describe('safeReturnTo', () => {
	it('keeps same-origin relative paths', () => {
		expect(safeReturnTo('/drafts/abc?x=1')).toBe('/drafts/abc?x=1');
	});

	it('rejects absolute and protocol-relative targets (no open redirect)', () => {
		for (const raw of ['https://evil.example', '//evil.example', '/\\evil.example', 'drafts', '']) {
			expect(safeReturnTo(raw)).toBe('/');
		}
		expect(safeReturnTo(null)).toBe('/');
	});
});
