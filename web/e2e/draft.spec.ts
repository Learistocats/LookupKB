// End-to-end proof for F2 (LOO-11 + LOO-13): log in → create draft → type
// content → it AUTOSAVES with no button press → reload → content persists → the
// private workspace lists it, through the REAL stack (web BFF → API → Postgres,
// email/password session). Plus an ownership spot-check: a second user cannot read the
// first user's draft (AC-2.3).
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

	test('autosave persists, workspace lists it, others cannot read it', async ({ page }) => {
		await login(page, USER!, PASS!);

		// Create a fresh draft; the app redirects into its editor at /drafts/{id}.
		await page.getByRole('button', { name: /new draft/i }).click();
		await expect(page).toHaveURL(/\/drafts\/[0-9a-f-]{36}$/);
		const draftUrl = page.url();

		// Give the draft a unique title so the workspace list can be asserted exactly.
		const stamp = Date.now();
		const titleMarker = `e2e-title-${stamp}`;
		const bodyMarker = `e2e-body-${stamp}`;
		await page.locator('input[name="title"]').fill(titleMarker);

		// The editor is CodeMirror 6 (LOO-12): type into its contenteditable surface.
		// The <textarea name="source_md"> still exists as the hidden form field / no-JS
		// fallback, and its value tracks the editor — so persistence is asserted against
		// it (a contenteditable has no toHaveValue).
		const editor = page.locator('.cm-content');
		await editor.click();
		await editor.pressSequentially(bodyMarker);

		// AC-2.6: autosave persists after the debounce with NO explicit Save click, and
		// the indicator visibly reaches a saved state.
		await expect(page.getByText(/^saved/i)).toBeVisible();

		// Reload: content must come back from Postgres, not in-memory state.
		await page.reload();
		await expect(page.locator('textarea[name="source_md"]')).toHaveValue(bodyMarker);
		await expect(page.locator('input[name="title"]')).toHaveValue(titleMarker);

		// AC-2.5: the private workspace lists this draft by its title; opening it
		// returns to the editor.
		await page.goto('/drafts');
		const entry = page.getByRole('link', { name: new RegExp(titleMarker) });
		await expect(entry).toBeVisible();
		await entry.click();
		await expect(page).toHaveURL(draftUrl);

		// Live preview (AC-3.1 / T-3.1): a Markdown heading on a non-active line
		// renders as formatted text via a CodeMirror decoration, not raw "# ".
		await editor.click();
		await page.keyboard.press('ControlOrMeta+a');
		await page.keyboard.press('Delete');
		await editor.pressSequentially('# Heading');
		await page.keyboard.press('Enter');
		await editor.pressSequentially('body');
		await expect(page.locator('.cm-preview-h1')).toBeVisible();

		// Ownership: a second user must not be able to read the draft, nor see it in
		// their own workspace (AC-2.3).
		if (USER2 && PASS2) {
			const ctx = await page.context().browser()!.newContext({ ignoreHTTPSErrors: true });
			const other = await ctx.newPage();
			await login(other, USER2, PASS2);
			await other.goto(draftUrl);
			await expect(other.getByText(/not found/i)).toBeVisible();
			await other.goto('/drafts');
			await expect(other.getByText(titleMarker)).toHaveCount(0);
			await ctx.close();
		}
	});

	// F5: the publish gate is a deliberate two-step confirm with server-side metadata
	// validation. T-5.1 (confirm step shows the metadata), T-5.2 (invalid metadata →
	// per-field errors, document stays a draft), then a valid publish → published.
	test('publish requires a confirm step, blocks on invalid metadata, then publishes', async ({
		page
	}) => {
		await login(page, USER!, PASS!);

		await page.getByRole('button', { name: /new draft/i }).click();
		await expect(page).toHaveURL(/\/drafts\/[0-9a-f-]{36}$/);

		const stamp = Date.now();
		const titleMarker = `e2e-pub-${stamp}`;
		await page.locator('input[name="title"]').fill(titleMarker);
		const editor = page.locator('.cm-content');
		await editor.click();
		await editor.pressSequentially('Some publishable body content.');
		await expect(page.getByText(/^saved/i)).toBeVisible();

		// T-5.1: clicking Publish opens a confirm step showing the metadata (the title
		// carries over into the dialog); publishing has NOT happened yet.
		await page.getByRole('button', { name: /^publish…$/i }).click();
		const dialog = page.getByRole('dialog', { name: /publish document/i });
		await expect(dialog).toBeVisible();
		await expect(dialog.getByText(/access:\s*public/i)).toBeVisible();

		// T-5.2: confirm with no tags → the server blocks publish with a per-field tag
		// error and the document is still a draft (the dialog stays open).
		await dialog.getByRole('button', { name: /confirm publish/i }).click();
		await expect(dialog.locator('.badge.error').filter({ hasText: /tag/i })).toBeVisible();
		await expect(dialog).toBeVisible();

		// Provide valid metadata, then confirm → the document becomes published.
		await dialog.getByText('Description', { exact: false }); // ensure the field is present
		await dialog.locator('textarea').fill('A short but sufficient description of the doc.');
		await dialog.locator('input[placeholder="Add a tag…"]').fill('e2e');
		await dialog.locator('input[placeholder="Add a tag…"]').press('Enter');
		await dialog.getByRole('button', { name: /confirm publish/i }).click();

		await expect(page.locator('.status.published')).toBeVisible();
		await expect(page.getByRole('button', { name: /^unpublish$/i })).toBeVisible();
	});

	// F8: a published document is readable at /docs/{slug} — the server serves its
	// pre-rendered, sanitized HTML (AC-8.1: the Markdown heading is a real <h1>, not
	// raw "# ") plus minimal metadata (AC-8.5: tags + version). This publishes a doc,
	// reads its title-derived slug off the editor, then opens the read view.
	test('a published document is readable at its slug with rendered HTML and metadata', async ({
		page
	}) => {
		await login(page, USER!, PASS!);

		await page.getByRole('button', { name: /new draft/i }).click();
		await expect(page).toHaveURL(/\/drafts\/[0-9a-f-]{36}$/);

		const stamp = Date.now();
		const titleMarker = `e2e-read-${stamp}`;
		const headingMarker = `Read Heading ${stamp}`;
		await page.locator('input[name="title"]').fill(titleMarker);
		const editor = page.locator('.cm-content');
		await editor.click();
		await editor.pressSequentially(`# ${headingMarker}`);
		await page.keyboard.press('Enter');
		await editor.pressSequentially('Readable body paragraph.');
		await expect(page.getByText(/^saved/i)).toBeVisible();

		// Publish with valid metadata (title carries over from the editor).
		await page.getByRole('button', { name: /^publish…$/i }).click();
		const dialog = page.getByRole('dialog', { name: /publish document/i });
		await expect(dialog).toBeVisible();
		await dialog.locator('textarea').fill('A short but sufficient description of the doc.');
		await dialog.locator('input[placeholder="Add a tag…"]').fill('read-e2e');
		await dialog.locator('input[placeholder="Add a tag…"]').press('Enter');
		await dialog.getByRole('button', { name: /confirm publish/i }).click();
		await expect(page.locator('.status.published')).toBeVisible();

		// On first publish the placeholder draft-<hex> slug becomes the title-derived
		// slug; read it off the editor's metadata line, then open the read view.
		const slug = (await page.locator('.meta code').innerText()).trim();
		expect(slug).not.toMatch(/^draft-/);
		await page.goto(`/docs/${slug}`);

		// AC-8.1: pre-rendered sanitized HTML — the Markdown heading renders as a real
		// heading element, and the body paragraph is present.
		await expect(page.getByRole('heading', { name: headingMarker })).toBeVisible();
		await expect(page.getByText('Readable body paragraph.')).toBeVisible();

		// AC-8.5: minimal metadata — the tag chip is shown.
		await expect(page.getByText('read-e2e')).toBeVisible();
	});
});

// login signs in through the app's email/password form. The accounts must exist
// already (create them once via /signup).
async function login(page: Page, email: string, password: string) {
	// Unauthenticated visits are redirected to the app's own /login form.
	await page.goto('/');
	await expect(page).toHaveURL(/\/login/);
	await page.locator('input[name="email"]').fill(email);
	await page.locator('input[name="password"]').fill(password);
	await page.getByRole('button', { name: /sign in/i }).click();

	// Signed in — reaching the app proves the session cookie was set and accepted.
	await expect(page.getByRole('button', { name: /new draft/i })).toBeVisible({ timeout: 15000 });
}
