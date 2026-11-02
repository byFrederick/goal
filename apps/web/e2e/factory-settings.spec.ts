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

test('factory settings has Save changes in the top bar and flat sections', async ({ page }) => {
  await page.goto('/o/acme/f/goal/settings');
  await expect(page.getByRole('heading', { level: 1, name: 'Settings' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Save changes' })).toBeVisible();
  await expect(page.locator('main [aria-expanded]')).toHaveCount(0);
  await expect(page.getByText('Badge', { exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: 'General' })).toHaveAttribute('aria-current', 'true');
});

// The pictures for the pull request: this screen at 1440 and 390 px, in both themes. They are attached to
// the report, not compared; docs/design/screens/build/factory-settings.jpg is the reference.
for (const theme of ['light', 'dark'] as const) {
  for (const width of [1440, 390]) {
    test(`factory settings screenshot, ${width} px, ${theme}`, async ({ page }, testInfo) => {
      await page.setViewportSize({ width, height: 900 });
      await page.addInitScript((value) => window.localStorage.setItem('goal-theme', value), theme);
      await page.goto('/o/acme/f/goal/settings');
      await expect(page.getByRole('heading', { level: 1, name: 'Settings' })).toBeVisible();
      await expect(page.getByRole('region', { name: 'Repository' })).toBeVisible();
      await testInfo.attach(`factory-settings-${width}-${theme}`, {
        body: await page.screenshot({ fullPage: true }),
        contentType: 'image/png',
      });
    });
  }
}
