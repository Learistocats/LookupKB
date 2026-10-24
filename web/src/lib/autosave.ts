// Autosave scheduler for the draft editor: framework-agnostic so the cadence and the
// dirty/saved state machine are testable with fake timers.
//
// A save fires after the author pauses (idle debounce) or every `intervalMs` while
// still dirty, whichever comes first, so continuous typing still persists.
//
// State follows the save request lifecycle, never a timer: a rejected save becomes
// `error`, stays dirty and re-arms for a retry. A false "saved" would hide data loss.

import { relativeTime } from './time';

export type SaveStatus = 'idle' | 'dirty' | 'saving' | 'saved' | 'error';

export interface AutosaveState {
	status: SaveStatus;
	/** Epoch ms of the last successful save, or null if nothing has saved yet. */
	lastSavedAt: number | null;
}

export interface AutosaveOptions<S> {
	/** Persist a snapshot. Rejects on failure (network/API error). */
	save: (snapshot: S) => Promise<void>;
	/** Called on every state transition so the UI can render the indicator. */
	onState: (state: AutosaveState) => void;
	/** Idle debounce before a save fires (ms). Default 1500. */
	debounceMs?: number;
	/** Periodic fallback: max time a change can stay unsaved while editing (ms). Default 15000. */
	intervalMs?: number;
	/** Injectable clock (tests). Default Date.now. */
	now?: () => number;
}

export interface Autosave<S> {
	/** Record a new snapshot: marks dirty and (re)arms the timers. */
	update(snapshot: S): void;
	/** Save any pending change immediately; resolves once nothing is pending. */
	flush(): Promise<void>;
	/** Stop all timers; further update()/flush() calls are no-ops. */
	destroy(): void;
}

export function createAutosave<S>(opts: AutosaveOptions<S>): Autosave<S> {
	const debounceMs = opts.debounceMs ?? 1500;
	const intervalMs = opts.intervalMs ?? 15000;
	const now = opts.now ?? (() => Date.now());

	let latest: S;
	let hasLatest = false;
	let pending = false; // unsaved changes exist
	let inFlight: Promise<void> | null = null;
	let lastSavedAt: number | null = null;
	let debounceTimer: ReturnType<typeof setTimeout> | undefined;
	let maxTimer: ReturnType<typeof setTimeout> | undefined;
	let destroyed = false;

	function emit(status: SaveStatus) {
		opts.onState({ status, lastSavedAt });
	}

	function clearTimers() {
		clearTimeout(debounceTimer);
		debounceTimer = undefined;
		clearTimeout(maxTimer);
		maxTimer = undefined;
	}

	// Reset the idle debounce on every keystroke, but leave the periodic ceiling
	// running once armed so continuous typing can't defer a save forever.
	function armTimers() {
		clearTimeout(debounceTimer);
		debounceTimer = setTimeout(() => void run(), debounceMs);
		if (maxTimer === undefined) {
			maxTimer = setTimeout(() => void run(), intervalMs);
		}
	}

	function run(): Promise<void> {
		if (destroyed) return Promise.resolve();
		// A save is already in flight — don't overlap; `pending` stays set so the
		// completion handler re-runs with the latest snapshot.
		if (inFlight) return inFlight;
		if (!pending || !hasLatest) return Promise.resolve();

		clearTimers();
		const snap = latest;
		pending = false;
		emit('saving');

		const p = Promise.resolve()
			.then(() => opts.save(snap))
			.then(() => {
				inFlight = null;
				lastSavedAt = now();
				if (pending) {
					// Edits arrived while saving — keep going.
					emit('dirty');
					armTimers();
				} else {
					emit('saved');
				}
			})
			.catch(() => {
				// Snapshot did NOT persist: stay dirty, surface the error, and re-arm so
				// the periodic fallback retries. Never emit `saved` on failure.
				inFlight = null;
				pending = true;
				emit('error');
				armTimers();
			});
		inFlight = p;
		return p;
	}

	function update(snapshot: S) {
		if (destroyed) return;
		latest = snapshot;
		hasLatest = true;
		pending = true;
		if (!inFlight) emit('dirty');
		armTimers();
	}

	async function flush(): Promise<void> {
		if (destroyed) return;
		clearTimers();
		if (inFlight) await inFlight;
		if (pending) await run();
	}

	function destroy() {
		destroyed = true;
		clearTimers();
	}

	return { update, flush, destroy };
}

export interface SaveIndicator {
	text: string;
	cls: 'idle' | 'saving' | 'dirty' | 'error' | 'ok';
}

/** saveIndicator maps the autosave state to the editor's status text. */
export function saveIndicator(
	state: AutosaveState,
	persisted: boolean,
	now: number
): SaveIndicator {
	const { status, lastSavedAt } = state;
	if (!persisted && status !== 'saving' && status !== 'error') {
		return { text: 'Not saved until you write something', cls: 'idle' };
	}
	switch (status) {
		case 'saving':
			return { text: 'Saving…', cls: 'saving' };
		case 'dirty':
			return { text: 'Unsaved changes', cls: 'dirty' };
		case 'error':
			return { text: 'Couldn’t save — retrying', cls: 'error' };
		default: {
			if (!lastSavedAt) return { text: 'All changes saved', cls: 'idle' };
			const ago = relativeTime(lastSavedAt, now);
			return { text: `Saved ${ago === 'now' ? 'just now' : `${ago} ago`}`, cls: 'ok' };
		}
	}
}
