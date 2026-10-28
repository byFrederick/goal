// Factory settings (build/factory-settings): the page's sections, the disabled people controls and the
// Pause and Resume round trip, at every width against the mock. The first factory of the mock seed is
// `goal`, which has a purpose.
import { expect, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll, expectToast, recordToasts } from './support';

test.beforeEach(async ({ page, request }) => {
  await request.post('/__mock/reset');
  await recordToasts(page);
});

test('factory settings is accessible and fits the screen', async ({ page }) => {
  await page.goto('/o/acme/f/goal/settings');
  await expect(page.getByRole('heading', { level: 1, name: 'Settings' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'General' })).toBeVisible();
  await expect(page.getByLabel('Name')).toHaveValue('goal');
  await expect(page.getByLabel('Purpose')).toHaveAttribute('readonly', '');
  await expect(page.getByRole('region', { name: 'People with access' })).toBeVisible();
  await expect(page.getByRole('region', { name: 'People with access' }).getByText('Organization owner')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Add people' })).toBeEnabled();
  await expect(page.getByRole('heading', { name: 'Danger zone' })).toBeVisible();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('pausing and resuming the factory from the Danger zone', async ({ page }) => {
  await page.goto('/o/acme/f/goal/settings');
  await page.getByRole('button', { name: 'Pause' }).click();
  await expectToast(page, 'Paused goal');
  await page.getByRole('button', { name: 'Resume' }).click();
  await expectToast(page, 'Resumed goal');
  await expect(page.getByRole('button', { name: 'Pause' })).toBeVisible();
});
