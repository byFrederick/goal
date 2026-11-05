// Secrets (ADR 0037) against the mock: acme's nine secrets and slack-bot as the Secrets picture shows them, the filters in the
// URL, the New secret sheet, a secret's page with Replace value, settings and a delete that a use blocks, and
// Import with its dry run. No value is read back from anywhere. Not run in the box (no browser): it runs in CI with
// `task web:e2e`.
import { expect, type Page, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll, isPhone, recordToasts } from './support';

test.beforeEach(async ({ page, request }) => {
  await request.post('/__mock/reset');
  await recordToasts(page);
});

/**
 * The list's entries as the width shows them: table rows from 768 px, linked cards on a phone
 * (secrets-page.tsx).
 */
const secretRows = (page: Page) =>
  isPhone(page)
    ? page
        .getByRole('main')
        .getByRole('listitem')
        .filter({ has: page.locator('a[href^="/o/acme/secrets/"]') })
    : page.getByRole('table').getByRole('row');

test.describe('the Secrets list', () => {
  test('lists the eleven secrets, warns about the one that expires soon, and shows no value', async ({ page }) => {
    await page.goto('/o/acme/secrets');
    await expect(page).toHaveTitle('Secrets · Acme · Goal');

    // The mock's eleven secrets, and a header row on a table.
    if (!isPhone(page)) await expect(page.getByRole('table', { name: 'Secrets of Acme' })).toBeVisible();
    await expect(secretRows(page)).toHaveCount(isPhone(page) ? 11 : 12);
    await expect(secretRows(page).filter({ hasText: 'cloudflare-deploy' })).toContainText('3 days');
    await expect(page.getByRole('region', { name: 'Expiring' })).toContainText('cloudflare-deploy expires in 3 days');
    await expectAccessible(page);
    await expectNoHorizontalScroll(page);
  });

  test('keeps the filter in the URL', async ({ page }) => {
    await page.goto('/o/acme/secrets');
    await page.getByRole('radio', { name: /Not used/ }).check();

    await expect(page).toHaveURL(/[?&]filter=unused/);
    await expect(secretRows(page)).toHaveCount(isPhone(page) ? 1 : 2);
  });

  test('creates a secret in the side sheet and lists it', async ({ page }) => {
    await page.goto('/o/acme/secrets');
    await page.getByRole('button', { name: 'New secret' }).click();
    const sheet = page.getByRole('dialog', { name: 'New secret' });
    await sheet.getByLabel('Name').fill('e2e-token');
    await sheet.getByLabel('Value', { exact: true }).fill('test-token');
    // A new secret reaches every factory until that is switched off.
    await sheet.getByLabel('All factories, including new ones').uncheck();
    // The mock titles two factories (goal and software) "Software"; the badge initials are hidden from the name.
    await sheet.getByRole('button', { name: 'Software', exact: true }).last().click();
    await expectAccessible(page);
    await sheet.getByRole('button', { name: 'Add secret' }).click();

    await expect(sheet).toBeHidden();
    await expect(secretRows(page).filter({ hasText: 'e2e-token' })).toBeVisible();
    await expect(page.getByText('test-token')).toHaveCount(0);
  });

  test('shows a taken name on the form', async ({ page }) => {
    await page.goto('/o/acme/secrets?new=true');
    const sheet = page.getByRole('dialog', { name: 'New secret' });
    await sheet.getByLabel('Name').fill('claude');
    await sheet.getByLabel('Value', { exact: true }).fill('test-token');
    await sheet.getByRole('button', { name: 'Add secret' }).click();

    await expect(sheet.getByRole('alert')).toContainText('already exists');
  });
});

test('the New secret sheet\u2019s Close button is 44 px tall on a phone', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop-1440', 'one project runs the phone size');
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/o/acme/secrets');
  await page.getByRole('button', { name: 'New secret' }).click();
  const close = page.getByRole('dialog', { name: 'New secret' }).getByRole('button', { name: 'Close' });
  const box = await close.boundingBox();
  expect(box?.height ?? 0).toBeGreaterThanOrEqual(44);
  await expectAccessible(page);
});

test.describe('a secret’s page', () => {
  test('shows where it is used and its activity, with a Delete that says why it is disabled', async ({ page }) => {
    await page.goto('/o/acme/secrets/github-token');
    await expect(page.getByRole('region', { name: /Where it’s used/ })).toContainText('5 places');
    await expect(page.getByRole('region', { name: 'Activity' })).toContainText('Created by');
    await expect(page.getByRole('button', { name: 'Delete secret' })).toHaveAttribute('aria-disabled', 'true');
    await expect(page.getByText('Remove it from the 5 places above first.')).toBeVisible();
    await expectAccessible(page);
    await expectNoHorizontalScroll(page);
  });

  test('replaces the value and shows it in the activity', async ({ page }) => {
    await page.goto('/o/acme/secrets/claude');
    await page.getByRole('region', { name: 'Value' }).getByRole('button', { name: 'Replace value' }).click();
    const dialog = page.getByRole('dialog', { name: /Replace the value of claude/ });
    await dialog.getByLabel('Value', { exact: true }).fill('test-token');
    await dialog.getByRole('button', { name: 'Replace value' }).click();

    await expect(dialog).toBeHidden();
    await expect(page.getByText('Value replaced on claude')).toBeVisible();
    await expect(
      page
        .getByRole('region', { name: 'Activity' })
        .getByText(/^Replaced by/)
        .first(),
    ).toBeVisible();
  });

  test('saves the settings', async ({ page }) => {
    await page.goto('/o/acme/secrets/claude');
    await page.getByRole('region', { name: 'Settings' }).getByRole('button', { name: 'Edit' }).click();
    await page.getByLabel('Description').fill('Shared subscription token');
    await page.getByRole('button', { name: 'Save' }).click();

    await expect(page.getByText('Shared subscription token')).toBeVisible();
  });

  test('deletes a secret nothing uses, after asking, and returns to the list', async ({ page }) => {
    await page.goto('/o/acme/secrets/old-npm-token');
    await page.getByRole('button', { name: 'Delete secret' }).click();
    await page
      .getByRole('dialog', { name: /Delete old-npm-token/ })
      .getByRole('button', { name: 'Delete secret' })
      .click();

    await expect(page).toHaveURL(/\/o\/acme\/secrets$/);
    await expect(page.getByRole('main')).toBeVisible();
    await expect(secretRows(page).filter({ hasText: 'old-npm-token' })).toHaveCount(0);
  });
});

test.describe('Import secrets', () => {
  test('dry-runs the text, then imports the new names and skips the existing ones', async ({ page }) => {
    await page.goto('/o/acme/secrets/import');
    await page.getByLabel('Paste .env or YAML').fill('SENTRY_TOKEN=test-a\nLINEAR_API_KEY=test-b\nNPM_TOKEN=test-c');

    const happens = page.getByRole('region', { name: 'What happens' });
    await expect(happens).toContainText('3 found, 2 new, 1 already exists');
    await expect(happens.getByText('Already exists', { exact: true })).toBeVisible();
    await expectAccessible(page);
    await page.getByRole('button', { name: 'Import 2' }).click();

    await expect(page).toHaveURL(/\/o\/acme\/secrets$/);
    await expect(secretRows(page).filter({ hasText: 'linear-api-key' })).toBeVisible();
  });

  test('the old Credentials addresses open Secrets and Import', async ({ page }) => {
    await page.goto('/o/acme/credentials');
    await expect(page).toHaveURL(/\/o\/acme\/secrets$/);
    await page.goto('/o/acme/credentials/new');
    await expect(page).toHaveURL(/\/o\/acme\/secrets\/import$/);
  });
});
