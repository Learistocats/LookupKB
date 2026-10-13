// End-to-end proof for F2/F5/F7/F8: log in → create an entry → type content → it
// AUTOSAVES with no button press → reload → content persists → the history panel
// pins it as a draft → publish → read → find it through search, through the REAL stack (web BFF → API → Postgres,
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

	test('autosave persists, the panel pins the draft, others cannot read it', async ({ page }) => {
		await login(page, USER!, PASS!);

		// The home page's + button creates an entry and opens it at /entries/{id}.
		await newEntry(page);
		const draftUrl = page.url();

		const stamp = Date.now();
		const titleMarker = `e2e-title-${stamp}`;
		const bodyMarker = `e2e-body-${stamp}`;
		await page.locator('input[name="title"]').fill(titleMarker);

		// The editor is CodeMirror 6: type into its contenteditable surface. The
		// <textarea name="source_md"> (no-JS fallback) tracks the editor, so
		// persistence is asserted against it.
		const editor = page.locator('.cm-content');
		await editor.click();
		await editor.pressSequentially(bodyMarker);

		// AC-2.6: autosave persists with NO explicit Save click.
		await expect(page.getByText(/^saved/i)).toBeVisible();

		// Reload: content must come back from Postgres, not in-memory state.
		await page.reload();
		await expect(page.locator('textarea[name="source_md"]')).toHaveValue(bodyMarker);
		await expect(page.locator('input[name="title"]')).toHaveValue(titleMarker);

		// AC-2.5: the history panel lists the draft under Drafts, marked as such.
		await page.getByRole('button', { name: /show your entries/i }).click();
		const panel = page.getByRole('complementary', { name: /your entries/i });
		const entry = panel.getByRole('link', { name: new RegExp(titleMarker) });
		await expect(entry).toBeVisible();
		await expect(entry.getByText('Draft', { exact: true })).toBeVisible();

		// Live preview (AC-3.1): a heading on a non-active line renders formatted.
		await editor.click();
		await page.keyboard.press('ControlOrMeta+a');
		await page.keyboard.press('Delete');
		await editor.pressSequentially('# Heading');
		await page.keyboard.press('Enter');
		await editor.pressSequentially('body');
		await expect(page.locator('.cm-preview-h1')).toBeVisible();

		// Ctrl/⌘+K opens search from anywhere — even with the editor focused.
		await page.keyboard.press('ControlOrMeta+k');
		await expect(page).toHaveURL(/\/$/);
		await expect(page.locator('#q')).toBeFocused();

		// Ownership (AC-2.3): another user can neither open the draft nor see it.
		if (USER2 && PASS2) {
			const ctx = await page.context().browser()!.newContext({ ignoreHTTPSErrors: true });
			const other = await ctx.newPage();
			await login(other, USER2, PASS2);
			await other.goto(draftUrl);
			await expect(other.getByText(/not found/i)).toBeVisible();
			await other.goto('/');
			await other.getByRole('button', { name: /show your entries/i }).click();
			await expect(other.getByText(titleMarker)).toHaveCount(0);
			await ctx.close();
		}
	});

	// F5: publishing is a deliberate two-step confirm with server-side validation.
	test('publish requires a confirm step, blocks on invalid metadata, then publishes', async ({
		page
	}) => {
		await login(page, USER!, PASS!);
		await newEntry(page);

		const titleMarker = `e2e-pub-${Date.now()}`;
		await page.locator('input[name="title"]').fill(titleMarker);
		const editor = page.locator('.cm-content');
		await editor.click();
		await editor.pressSequentially('Some publishable body content.');
		await expect(page.getByText(/^saved/i)).toBeVisible();

		// T-5.1: Publish opens a confirm step showing the metadata; nothing is
		// published yet.
		await page.getByRole('button', { name: /^publish…$/i }).click();
		const dialog = page.getByRole('dialog', { name: /publish document/i });
		await expect(dialog).toBeVisible();
		await expect(dialog.getByText(/visible to everyone/i)).toBeVisible();

		// T-5.2: no tags → per-field error, the dialog stays open, still a draft.
		await dialog.getByRole('button', { name: /confirm publish/i }).click();
		await expect(dialog.locator('.error-text').filter({ hasText: /tag/i })).toBeVisible();
		await expect(dialog).toBeVisible();

		await dialog.locator('textarea').fill('A short but sufficient description of the doc.');
		await dialog.locator('input[placeholder="Add a tag…"]').fill('e2e');
		await dialog.locator('input[placeholder="Add a tag…"]').press('Enter');
		await dialog.getByRole('button', { name: /confirm publish/i }).click();

		await expect(page.locator('.topbar').getByText('Published', { exact: true })).toBeVisible();
		await expect(page.getByRole('button', { name: /^unpublish$/i })).toBeVisible();
	});

	// F8 + F7: a published entry is readable at its slug (pre-rendered HTML +
	// metadata) and becomes findable from the home search box.
	test('a published entry is readable and searchable', async ({ page }) => {
		await login(page, USER!, PASS!);
		await newEntry(page);

		const stamp = Date.now();
		const titleMarker = `e2e-read-${stamp}`;
		const headingMarker = `Read Heading ${stamp}`;
		await page.locator('input[name="title"]').fill(titleMarker);
		const editor = page.locator('.cm-content');
		await editor.click();
		await editor.pressSequentially(`# ${headingMarker}`);
		await page.keyboard.press('Enter');
		await editor.pressSequentially('Readable body paragraph about zeppelins.');
		await expect(page.getByText(/^saved/i)).toBeVisible();

		await page.getByRole('button', { name: /^publish…$/i }).click();
		const dialog = page.getByRole('dialog', { name: /publish document/i });
		await dialog.locator('textarea').fill('A short but sufficient description of the doc.');
		await dialog.locator('input[placeholder="Add a tag…"]').fill('read-e2e');
		await dialog.locator('input[placeholder="Add a tag…"]').press('Enter');
		await dialog.getByRole('button', { name: /confirm publish/i }).click();

		// "View" opens the reader at the title-derived slug.
		await page.getByRole('link', { name: /view/i }).click();
		await expect(page).toHaveURL(/\/docs\/e2e-read-/);

		// AC-8.1: the Markdown heading is a real heading; AC-8.5: the tag is shown.
		await expect(page.getByRole('heading', { name: headingMarker })).toBeVisible();
		await expect(page.getByText('Readable body paragraph about zeppelins.')).toBeVisible();
		await expect(page.getByText('read-e2e')).toBeVisible();
		// It's ours, so the reader offers Edit.
		await expect(page.getByRole('link', { name: /edit/i })).toBeVisible();

		// F7: the body text is indexed within seconds and found from the home page.
		// Indexing is asynchronous (outbox worker, ~1 s), and live search only
		// re-queries when the text changes, so retype until the entry shows up.
		await page.goto('/');
		const box = page.locator('#q');
		const hit = page.locator('.hit').filter({ hasText: titleMarker });
		await expect(async () => {
			await box.fill('');
			await box.fill(`zeppelins ${stamp}`);
			await expect(hit).toBeVisible({ timeout: 1500 });
		}).toPass({ timeout: 15000 });
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
	await expect(page.locator('#q')).toBeVisible({ timeout: 15000 });
}

// newEntry presses the home page's + button and waits for the editor.
async function newEntry(page: Page) {
	await page.goto('/');
	await page.getByRole('button', { name: 'New entry' }).last().click();
	await expect(page).toHaveURL(/\/entries\/[0-9a-f-]{36}$/);
}
