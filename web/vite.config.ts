import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vitest/config';

// API target for the dev server proxy. During local `pnpm dev` we proxy /api,
// /healthz and /readyz to the Go API so they stay same-origin (no CORS). In the
// compose stack the browser only talks to the SvelteKit server (the BFF), which
// calls the API server-side.
const apiTarget = process.env.VITE_API_PROXY_TARGET ?? 'http://localhost:8080';

export default defineConfig({
	plugins: [sveltekit()],
	server: {
		port: 5173,
		proxy: {
			'/healthz': apiTarget,
			'/readyz': apiTarget,
			'/api': apiTarget
		}
	},
	test: {
		include: ['src/**/*.{test,spec}.{js,ts}']
	}
});
