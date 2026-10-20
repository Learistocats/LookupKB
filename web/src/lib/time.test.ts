import { describe, it, expect } from 'vitest';
import { relativeTime } from './time';

describe('relativeTime', () => {
	const now = Date.parse('2026-10-13T12:00:00Z');
	it('buckets recent times', () => {
		expect(relativeTime('2026-10-13T11:59:30Z', now)).toBe('now');
		expect(relativeTime('2026-10-13T11:55:00Z', now)).toBe('5m');
		expect(relativeTime('2026-10-13T09:00:00Z', now)).toBe('3h');
		expect(relativeTime('2026-10-11T12:00:00Z', now)).toBe('2d');
	});
	it('returns empty for garbage', () => {
		expect(relativeTime('nope', now)).toBe('');
	});
});
