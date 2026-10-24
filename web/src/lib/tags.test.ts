import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { normalizeTag, validateTags, searchTags, debounce, resolveProtected } from './tags';

describe('normalizeTag', () => {
	it('lowercases and kebab-cases mixed input', () => {
		expect(normalizeTag('My Tag')).toBe('my-tag');
		expect(normalizeTag('  spaced  out  ')).toBe('spaced-out');
		expect(normalizeTag('snake_case')).toBe('snake-case');
		expect(normalizeTag('-leading-trailing-')).toBe('leading-trailing');
		expect(normalizeTag('dots.and/slashes')).toBe('dots-and-slashes');
	});

	it('keeps unicode letters and drops symbols', () => {
		expect(normalizeTag('Café Métrics')).toBe('café-métrics');
		expect(normalizeTag('emoji 🎉 party')).toBe('emoji-party');
		expect(normalizeTag('C++')).toBe('c');
	});

	it('is idempotent', () => {
		for (const s of ['My Tag', '  weird__x!! ', 'Café', 'already-kebab']) {
			expect(normalizeTag(normalizeTag(s))).toBe(normalizeTag(s));
		}
	});

	it('returns empty for punctuation-only input', () => {
		expect(normalizeTag('!!!')).toBe('');
		expect(normalizeTag('   ')).toBe('');
	});
});

describe('validateTags', () => {
	const limits = { minTags: 1, maxTags: 3, minLen: 2, maxLen: 5 };

	it('normalizes and de-dupes valid tags', () => {
		const { normalized, errors } = validateTags(['My Tag', 'my-tag'], { ...limits, maxLen: 32 });
		expect(normalized).toEqual(['my-tag']);
		expect(errors).toHaveLength(0);
	});

	it('flags too few and too many tags', () => {
		expect(validateTags([], limits).errors.join()).toMatch(/at least 1/);
		expect(validateTags(['aa', 'bb', 'cc', 'dd'], limits).errors.join()).toMatch(/at most 3/);
	});

	it('flags out-of-range tag length', () => {
		expect(validateTags(['a'], limits).errors.join()).toMatch(/2–5 characters/);
		expect(validateTags(['toolong'], limits).errors.join()).toMatch(/2–5 characters/);
	});

	it('flags an invalid (empty-normalizing) tag', () => {
		expect(validateTags(['!!!'], limits).errors.join()).toMatch(/not a valid tag/);
	});
});

describe('searchTags', () => {
	it('calls the /tags/search proxy and returns suggestions', async () => {
		const fetchFn = vi.fn().mockResolvedValue(
			new Response(JSON.stringify([{ slug: 'api', label: 'api', usage_count: 3 }]), {
				status: 200,
				headers: { 'content-type': 'application/json' }
			})
		);
		const hits = await searchTags('ap i', fetchFn as unknown as typeof fetch);
		expect(hits[0].slug).toBe('api');
		expect(fetchFn).toHaveBeenCalledWith('/tags/search?q=ap%20i');
	});

	it('throws on a non-ok response', async () => {
		const fetchFn = vi.fn().mockResolvedValue(new Response('nope', { status: 502 }));
		await expect(searchTags('x', fetchFn as unknown as typeof fetch)).rejects.toThrow('502');
	});
});

describe('debounce', () => {
	beforeEach(() => vi.useFakeTimers());
	afterEach(() => vi.useRealTimers());

	it('runs only once for a burst, with the latest args', () => {
		const fn = vi.fn();
		const d = debounce(fn, 200);
		d('a');
		d('b');
		d('c');
		expect(fn).not.toHaveBeenCalled();
		vi.advanceTimersByTime(200);
		expect(fn).toHaveBeenCalledTimes(1);
		expect(fn).toHaveBeenCalledWith('c');
	});
});

describe('resolveProtected', () => {
	function jsonFetch(bySlug: Record<string, unknown[]>) {
		return vi.fn(async (input: string | URL | Request) => {
			const q = new URL(String(input), 'http://x').searchParams.get('q') ?? '';
			return new Response(JSON.stringify(bySlug[q] ?? []), { status: 200 });
		}) as unknown as typeof fetch;
	}

	it('flags exact slug matches that are protected and defaults the rest to false', async () => {
		const fetchFn = jsonFetch({
			reeflect: [{ slug: 'reeflect', label: 'reeflect', usage_count: 1, protected: true }],
			go: [{ slug: 'golang', label: 'golang', usage_count: 3, protected: true }]
		});
		const out = await resolveProtected(['reeflect', 'go', 'brand-new'], fetchFn);
		expect(out).toEqual({ reeflect: true, go: false, 'brand-new': false });
	});
});
