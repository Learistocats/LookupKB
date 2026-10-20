import { describe, it, expect } from 'vitest';
import { nextTheme, parseTheme } from './theme';

describe('theme', () => {
	it('parses unknown values as system', () => {
		expect(parseTheme(undefined)).toBe('system');
		expect(parseTheme('neon')).toBe('system');
		expect(parseTheme('dark')).toBe('dark');
	});

	it('cycles system → light → dark → system', () => {
		expect(nextTheme('system')).toBe('light');
		expect(nextTheme('light')).toBe('dark');
		expect(nextTheme('dark')).toBe('system');
	});
});
