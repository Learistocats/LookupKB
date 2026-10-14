import { describe, expect, it } from 'vitest';
import { isBlankEntry } from './entries';

describe('isBlankEntry', () => {
	it('is blank when nothing was written', () => {
		expect(isBlankEntry({ title: null, description: null, source_md: null })).toBe(true);
		expect(isBlankEntry({ title: '', description: '  ', source_md: '\n\t' })).toBe(true);
	});

	it('is not blank once any field has content', () => {
		expect(isBlankEntry({ title: 'T', description: null, source_md: null })).toBe(false);
		expect(isBlankEntry({ title: null, description: 'd', source_md: null })).toBe(false);
		expect(isBlankEntry({ title: null, description: null, source_md: '# x' })).toBe(false);
	});
});
