import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createAutosave, type AutosaveState } from './autosave';

type Snap = { v: number };

function setup(opts: { saveImpl?: (s: Snap) => Promise<void> } = {}) {
	const states: AutosaveState[] = [];
	const saves: Snap[] = [];
	const saveImpl = opts.saveImpl ?? (async () => {});
	const save = vi.fn(async (s: Snap) => {
		saves.push(s);
		await saveImpl(s);
	});
	const a = createAutosave<Snap>({
		save,
		onState: (s) => states.push({ ...s }),
		debounceMs: 1000,
		intervalMs: 5000
	});
	return { a, states, saves, save };
}

const statuses = (states: AutosaveState[]) => states.map((s) => s.status);

describe('createAutosave', () => {
	beforeEach(() => vi.useFakeTimers());
	afterEach(() => vi.useRealTimers());

	it('saves once after the idle debounce, with the latest snapshot', async () => {
		const { a, saves, states } = setup();
		a.update({ v: 1 });
		a.update({ v: 2 });
		expect(saves).toHaveLength(0);

		await vi.advanceTimersByTimeAsync(1000);

		expect(saves).toEqual([{ v: 2 }]);
		expect(states.at(-1)?.status).toBe('saved');
		expect(states.at(-1)?.lastSavedAt).not.toBeNull();
	});

	it('never saves without an edit (opening a draft stays clean)', async () => {
		const { saves } = setup();
		await vi.advanceTimersByTimeAsync(10_000);
		expect(saves).toHaveLength(0);
	});

	it('periodic fallback persists even while the user keeps typing', async () => {
		const { a, saves } = setup(); // debounce 1000, interval 5000
		// Type every 800ms so the idle debounce never elapses on its own.
		for (let i = 0; i < 10; i++) {
			a.update({ v: i });
			await vi.advanceTimersByTimeAsync(800);
		}
		// ~8000ms of continuous typing must have tripped the 5000ms ceiling at least once.
		expect(saves.length).toBeGreaterThanOrEqual(1);
	});

	it('flush() saves the pending change immediately, without a timer', async () => {
		const { a, saves } = setup();
		a.update({ v: 7 });
		await a.flush();
		expect(saves).toEqual([{ v: 7 }]);
	});

	it('surfaces an error and stays dirty on a failed save (never a false "saved")', async () => {
		const { a, states, saves } = setup({
			saveImpl: async () => {
				throw new Error('boom');
			}
		});
		a.update({ v: 1 });
		await vi.advanceTimersByTimeAsync(1000);

		expect(saves).toHaveLength(1);
		expect(statuses(states)).toContain('error');
		expect(statuses(states)).not.toContain('saved');
		expect(states.at(-1)?.status).toBe('error');
		expect(states.at(-1)?.lastSavedAt).toBeNull();
	});

	it('does not overlap saves; edits during a save coalesce to the latest snapshot', async () => {
		let release: () => void = () => {};
		const gate = new Promise<void>((r) => (release = r));
		const { a, saves } = setup({ saveImpl: () => gate });

		a.update({ v: 1 });
		await vi.advanceTimersByTimeAsync(1000); // first save starts, blocked on the gate
		expect(saves).toEqual([{ v: 1 }]);

		a.update({ v: 2 }); // edits arrive mid-flight
		a.update({ v: 3 });
		await vi.advanceTimersByTimeAsync(1000); // debounce would fire, but a save is in flight
		expect(saves).toEqual([{ v: 1 }]); // no overlap

		release();
		await vi.advanceTimersByTimeAsync(1000); // first save resolves → re-arm → save latest
		expect(saves).toEqual([{ v: 1 }, { v: 3 }]);
	});

	it('stops all work after destroy()', async () => {
		const { a, saves } = setup();
		a.update({ v: 1 });
		a.destroy();
		await vi.advanceTimersByTimeAsync(10_000);
		expect(saves).toHaveLength(0);
	});
});
