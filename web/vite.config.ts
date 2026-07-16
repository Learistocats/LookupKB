import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

// API target for the dev server proxy. In `docker compose` the browser reaches
// the API through Caddy at /api and /healthz; during local `pnpm dev` we proxy
// those same paths to the Go API so the placeholder page can call /healthz
// same-origin (no CORS).
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
