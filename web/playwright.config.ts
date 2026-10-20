import { defineConfig, devices } from '@playwright/test';

// The walking-skeleton E2E (draft.spec.ts) drives the REAL stack — web BFF → API
// → Postgres with an email/password session — because that round trip is exactly
// what LOO-11 must prove (mocks are explicitly out of scope). It therefore does
// NOT start its own server; point it at a running stack:
//
//   E2E_BASE_URL=http://localhost:3000 \
//   E2E_USERNAME=<email> E2E_PASSWORD=... \
//   E2E_SECOND_USERNAME=... E2E_SECOND_PASSWORD=... \
//   pnpm test:e2e
//
// http://localhost:3000 is the web container's default published port; for a
// TLS deployment pass the https base URL.
// Without E2E_BASE_URL the suite is skipped (see draft.spec.ts) so `pnpm test:e2e`
// is safe to run in CI that has no stack. Browsers: `pnpm exec playwright install chromium`.
const baseURL = process.env.E2E_BASE_URL ?? 'http://localhost:3000';

export default defineConfig({
	testDir: './e2e',
	fullyParallel: false,
	forbidOnly: !!process.env.CI,
	retries: process.env.CI ? 1 : 0,
	workers: 1,
	reporter: 'list',
	use: {
		baseURL,
		trace: 'on-first-retry',
		// A host proxy in front may use a self-signed/internal CA cert.
		ignoreHTTPSErrors: true
	},
	projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }]
});
