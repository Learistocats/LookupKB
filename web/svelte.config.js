import adapter from '@sveltejs/adapter-node';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

/** @type {import('@sveltejs/kit').Config} */
const config = {
	preprocess: vitePreprocess(),
	kit: {
		// LookupKB web runs as a small Node server (the OIDC BFF): hooks.server.ts
		// owns the Authorization Code + PKCE flow and holds the session in an
		// encrypted HttpOnly cookie, so tokens never reach client-side JS
		// (docs/planning/01-architecture.md §3.1, 06-security.md §5). Caddy reverse
		// proxies to this server on :8080.
		adapter: adapter()
	}
};

export default config;
