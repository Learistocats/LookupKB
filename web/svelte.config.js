import adapter from '@sveltejs/adapter-node';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

/** @type {import('@sveltejs/kit').Config} */
const config = {
	preprocess: vitePreprocess(),
	kit: {
		// LookupKB web runs as a small Node server (the BFF): it signs users in
		// against the Go API and holds the API token in an encrypted HttpOnly
		// cookie, so tokens never reach client-side JS
		// (docs/planning/01-architecture.md §3.1, 06-security.md §5).
		adapter: adapter()
	}
};

export default config;
