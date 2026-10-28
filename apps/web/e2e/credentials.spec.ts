// Credentials (ADR 0017) against the mock: acme's seven credentials as the Credentials picture
// shows them, the type tabs in the URL, the expiry reminder with Replace, and a delete that a
// connection blocks. Not run in the box (no browser): it runs in CI with `task web:e2e`.
import { expect, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll, recordToasts } from './support';

test.beforeEach(async ({ page, request }) => {
  await request.post('/__mock/reset');
  await recordToasts(page);
});

test.describe('the Credentials page', () => {
  test('lists the seven credentials with their factories, users and expiry', async ({ page }) => {
    await page.goto('/o/acme/credentials');
    await expect(page).toHaveTitle('Credentials · Acme · Goal');
    await expect(page.getByRole('heading', { level: 1, name: 'Credentials' })).toBeVisible();
    await expect(page.getByRole('status')).toHaveCount(0);

    const table = page.getByRole('table', { name: 'Credentials of Acme' });
    await expect(table.getByRole('row')).toHaveCount(8);
    await expect(table.getByRole('row').filter({ hasText: 'cloudflare-token' })).toContainText('in 2 days');
    await expect(page.getByText('1 expires this week')).toBeVisible();
    await expectAccessible(page);
    await expectNoHorizontalScroll(page);
  });

  test('keeps the type tab in the URL', async ({ page }) => {
    await page.goto('/o/acme/credentials');
    await page.getByRole('radio', { name: /OAuth tokens/ }).check();

    await expect(page).toHaveURL(/[?&]type=oauth_token/);
    await expect(page.getByRole('table').getByRole('row')).toHaveCount(2);
  });

  test('replaces a value from the reminder window and sends it once', async ({ page }) => {
    await page.goto('/o/acme/credentials');
    const row = page.getByRole('row').filter({ hasText: 'cloudflare-token' });
    await row.getByRole('button', { name: 'Replace' }).click();

    const dialog = page.getByRole('dialog', { name: /Replace the value of cloudflare-token/ });
    await dialog.getByLabel('Value').fill('test-token');
    await dialog.getByRole('button', { name: 'Replace value' }).click();

    await expect(dialog).toBeHidden();
    await expect(page.getByText('Value replaced on cloudflare-token')).toBeVisible();
  });

  test('says which connection blocks a delete', async ({ page }) => {
    await page.goto('/o/acme/credentials');
    await page.getByRole('button', { name: 'Actions for cloudflare-token' }).click();
    await page.getByRole('menuitem', { name: 'Delete…' }).click();
    await page.getByRole('button', { name: 'Delete credential' }).click();

    await expect(page.getByRole('alert')).toContainText('in use by connections cloudflare');
  });

  test('shows encryption in the browser as not available yet', async ({ page }) => {
    await page.goto('/o/acme/credentials');
    const control = page.getByRole('button', { name: 'Encrypt in your browser instead' });

    await expect(control).toHaveAttribute('aria-disabled', 'true');
  });
});

test.describe('the New credential page', () => {
  test('creates one credential scoped to a factory and one of its agents', async ({ page }) => {
    await page.goto('/o/acme/credentials/new');
    await expect(page.getByRole('heading', { level: 1, name: 'New credential' })).toBeVisible();

    await page.getByLabel('Name').fill('e2e-token-2028');
    await page.getByLabel('Value').fill('test-token');
    await page.getByRole('button', { name: /Software/ }).click();
    await page.getByRole('checkbox', { name: 'architect' }).check();
    await expectAccessible(page);
    await page.getByRole('button', { name: 'Create credential' }).click();

    await expect(page).toHaveURL(/\/o\/acme\/credentials$/);
    await expect(page.getByRole('row').filter({ hasText: 'e2e-token-2028' })).toBeVisible();
    await expectNoHorizontalScroll(page);
  });

  test('imports the names of a pasted YAML after a dry run, skipping the existing one', async ({ page }) => {
    await page.goto('/o/acme/credentials/new?panel=import');
    await page
      .getByLabel('Paste .env or YAML')
      .fill('credentials:\n  sentry-token: test-1\n  linear-key: test-2\n  cloudflare-token: test-3\n');

    await expect(page.getByText('3 found')).toBeVisible();
    await expect(page.getByText('Already exists')).toBeVisible();
    await expect(page.getByText('Used by 2 connections')).toBeVisible();
    await page.getByRole('button', { name: 'Import 2' }).click();

    await expect(page).toHaveURL(/\/o\/acme\/credentials$/);
    await expect(page.getByRole('row').filter({ hasText: 'sentry-token' })).toBeVisible();
  });
});
