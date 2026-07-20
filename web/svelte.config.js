import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

/** @type {import('@sveltejs/kit').Config} */
const config = {
	preprocess: vitePreprocess(),
	kit: {
		// LookupKB web is a SPA of static assets served by Caddy
		// (see docs/planning/01-architecture.md §5.2). SSR is disabled in
		// src/routes/+layout.ts; adapter-static emits an index.html fallback.
		adapter: adapter({
			fallback: 'index.html',
			precompress: true,
			strict: false
		})
	}
};

export default config;
