// Walking-skeleton end-to-end proof (LOO-11): log in → create draft → type
// content → save → reload → content persists, through the REAL stack (Caddy →
// API → Postgres, Authentik session). Plus an ownership spot-check: a second
// user cannot read the first user's draft (AC-2.3).
//
// Runs only when E2E_BASE_URL + credentials are set (see playwright.config.ts);
// otherwise the whole suite is skipped so CI without a stack stays green.
import { test, expect, type Page } from '@playwright/test';

const BASE = process.env.E2E_BASE_URL;
const USER = process.env.E2E_USERNAME;
const PASS = process.env.E2E_PASSWORD;
const USER2 = process.env.E2E_SECOND_USERNAME;
const PASS2 = process.env.E2E_SECOND_PASSWORD;

test.describe('draft round-trip', () => {
	test.skip(!BASE || !USER || !PASS, 'set E2E_BASE_URL/E2E_USERNAME/E2E_PASSWORD to run');

	test('create, save, reload persists', async ({ page }) => {
		await login(page, USER!, PASS!);

		// Create a fresh draft; the app redirects into its editor at /drafts/{id}.
		await page.getByRole('button', { name: /new draft/i }).click();
		await expect(page).toHaveURL(/\/drafts\/[0-9a-f-]{36}$/);
		const draftUrl = page.url();

		// The editor is now CodeMirror 6 (LOO-12): type into its contenteditable
		// surface. The <textarea name="source_md"> still exists as the hidden form
		// field / no-JS fallback, and its value tracks the editor — so persistence is
		// asserted against it (a contenteditable has no toHaveValue).
		const editor = page.locator('.cm-content');
		const marker = `e2e-${Date.now()}`;
		await editor.click();
		await editor.pressSequentially(marker);
		await page.getByRole('button', { name: /^save$/i }).click();
		await expect(page.getByText(/saved/i)).toBeVisible();

		// Reload: content must come back from Postgres, not in-memory state.
		await page.reload();
		await expect(page.locator('textarea[name="source_md"]')).toHaveValue(marker);

		// Live preview (AC-3.1 / T-3.1): a Markdown heading on a non-active line
		// renders as formatted text via a CodeMirror decoration, not raw "# ".
		await editor.click();
		await page.keyboard.press('ControlOrMeta+a');
		await page.keyboard.press('Delete');
		await editor.pressSequentially('# Heading');
		await page.keyboard.press('Enter');
		await editor.pressSequentially('body');
		await expect(page.locator('.cm-preview-h1')).toBeVisible();

		// Ownership: a second user must not be able to read the draft (AC-2.3).
		if (USER2 && PASS2) {
			const ctx = await page.context().browser()!.newContext({ ignoreHTTPSErrors: true });
			const other = await ctx.newPage();
			await login(other, USER2, PASS2);
			await other.goto(draftUrl);
			await expect(other.getByText(/not found/i)).toBeVisible();
			await ctx.close();
		}
	});
});

// login performs the Authentik Authorization-Code flow through the browser. The
// app redirects an unauthenticated visitor to Authentik; we fill the identifier
// then password stage (Authentik's default flow), handle an optional consent
// screen, and land back on the app. Fields are targeted by their form `name`
// (Authentik's inputs carry no associated <label>); Playwright pierces the
// flow's web-component shadow DOM for name/role selectors.
async function login(page: Page, username: string, password: string) {
	await page.goto('/');

	// Identification stage: the visible identifier field is input[name="uidField"].
	// Submit with Enter — Authentik's flow submits reliably on Enter, whereas
	// clicking the freshly-rendered stage button can race the web-component swap.
	const uid = page.locator('input[name="uidField"]');
	await uid.waitFor({ state: 'visible' });
	await uid.fill(username);
	await uid.press('Enter');

	// Password stage.
	const pw = page.locator('input[name="password"]');
	await pw.waitFor({ state: 'visible' });
	await pw.fill(password);
	await pw.press('Enter');

	// Authentik then shows an auth-completion interstitial and an explicit-consent
	// screen, each with a primary submit ("Continue"). Click through the stages
	// while still on an Authentik flow page (URL contains /if/flow/); stop as soon
	// as we leave Authentik so we never click the app's own submit ("New draft" is
	// itself a submit button).
	for (let i = 0; i < 8 && page.url().includes('/if/flow/'); i++) {
		const submit = page.locator('button[type=submit]:visible').first();
		if (await submit.count()) await submit.click().catch(() => {});
		await page.waitForTimeout(1000);
	}

	// Signed in — reaching the app proves the (chunked) session cookie survived the
	// redirect back rather than being dropped for exceeding the per-cookie limit.
	await expect(page.getByRole('button', { name: /new draft/i })).toBeVisible({ timeout: 15000 });
}
