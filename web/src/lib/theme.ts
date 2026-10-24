// Theme preference: 'system' (follow the OS), 'light' or 'dark'. Stored in a
// cookie so the server can render the right data-theme on first paint.
export type ThemePref = 'system' | 'light' | 'dark';

export const THEME_COOKIE = 'lkb_theme';
export const PANEL_COOKIE = 'lkb_panel';

export function parseTheme(raw: string | undefined | null): ThemePref {
	return raw === 'light' || raw === 'dark' ? raw : 'system';
}

/** nextTheme cycles system → light → dark → system. */
export function nextTheme(t: ThemePref): ThemePref {
	return t === 'system' ? 'light' : t === 'light' ? 'dark' : 'system';
}

/** setPrefCookie persists a UI preference for a year. */
export function setPrefCookie(name: string, value: string | number): void {
	document.cookie = `${name}=${value}; path=/; max-age=31536000; samesite=lax`;
}

export function applyTheme(t: ThemePref): void {
	const root = document.documentElement;
	if (t === 'system') delete root.dataset.theme;
	else root.dataset.theme = t;
	setPrefCookie(THEME_COOKIE, t);
}
