// Compact relative times for lists ("now", "5m", "3h", "2d", then a date).
export function relativeTime(iso: string, now: number = Date.now()): string {
	const then = Date.parse(iso);
	if (Number.isNaN(then)) return '';
	const s = Math.max(0, Math.floor((now - then) / 1000));
	if (s < 60) return 'now';
	const m = Math.floor(s / 60);
	if (m < 60) return `${m}m`;
	const h = Math.floor(m / 60);
	if (h < 24) return `${h}h`;
	const d = Math.floor(h / 24);
	if (d < 7) return `${d}d`;
	return new Date(then).toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
}

/** Long date for article bylines, e.g. "13 October 2026". */
export function longDate(iso: string): string {
	const d = new Date(iso);
	if (Number.isNaN(d.getTime())) return iso;
	return d.toLocaleDateString(undefined, { year: 'numeric', month: 'long', day: 'numeric' });
}
