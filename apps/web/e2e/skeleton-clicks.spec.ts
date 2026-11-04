// A finished Skeleton leaves its placeholder layer in the page, faded out. It must not take clicks: a real
// Playwright click (never force) reaches the controls under it. Written for the Playwright suite, which
// cannot run in the box that built it.
import { expect, test } from '@playwright/test';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

test('a catalog entry on Add a tool takes a click and shows its tool', async ({ page }) => {
  await page.goto('/o/acme/f/goal/definition/add-tool?environment=default');
  await page.getByRole('link', { name: /Sentry/ }).click();
  await expect(page).toHaveURL(/template=sentry/);
  await expect(page.getByRole('button', { name: 'Add to draft' })).toBeVisible();
});

test('Use a secret under the finished Skeleton opens the secret picker', async ({ page }) => {
  await page.goto('/o/acme/f/goal/definition/add-tool?environment=default&template=custom&type=cli');
  await expect(page.getByRole('heading', { name: 'Custom tool' })).toBeVisible();
  await page.getByRole('button', { name: 'Add a variable' }).click();
  await page.getByRole('button', { name: 'Use a secret' }).click();
  await expect(page.getByLabel('Secret for row 1')).toBeVisible();
});

test('an MCP server row on the Environments page takes a click and opens', async ({ page }) => {
  await page.goto('/o/acme/f/goal/definition/environments');
  await expect(page.getByRole('heading', { name: 'The box' })).toBeVisible();
  await page
    .getByRole('button', { name: /linear/ })
    .first()
    .click();
  await expect(page.getByRole('button', { name: 'Revoke create_comment for architect' })).toBeVisible();
});
