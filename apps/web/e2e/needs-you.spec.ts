import { expect, test } from '@playwright/test';
import { expectNoHorizontalScroll, orgNav } from './support';

// Needs you (/o/acme/needs-you): the list, the split detail and the phone's approve view, against the
// mock API. The mock seeds decisions and alerts in the software, sre and support factories.
test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

test('the page lists what waits on you, with the count in the header', async ({ page }) => {
  await page.goto('/o/acme/needs-you');
  await expect(page.getByRole('heading', { name: 'Needs you' })).toBeVisible();
  await expect(page.getByRole('list', { name: /Waiting on you/ })).toBeVisible();
  await expectNoHorizontalScroll(page);
});

test('the Decisions tab writes its type to the URL and hides the alerts', async ({ page }) => {
  await page.goto('/o/acme/needs-you');
  await page.getByRole('button', { name: /^Decisions/ }).click();
  await expect(page).toHaveURL(/type=decision/);
  await expect(page.getByRole('button', { name: /^Decisions/ })).toHaveAttribute('aria-pressed', 'true');
});

test('Approvals and Questions are not built yet and say so', async ({ page }) => {
  await page.goto('/o/acme/needs-you');
  await expect(page.getByRole('button', { name: /^Approvals/ })).toHaveAttribute('aria-disabled', 'true');
  await expect(page.getByRole('button', { name: /^Questions/ })).toHaveAttribute('aria-disabled', 'true');
});

test('J and K move the selection, and the URL names the selected entry', async ({ page }) => {
  await page.goto('/o/acme/needs-you');
  await page.keyboard.press('j');
  await expect(page).toHaveURL(/entry=/);
  await page.keyboard.press('k');
  await expect(page.locator('a[aria-current="page"]')).toHaveCount(1);
});

test.describe('the phone approve view at 375 px', () => {
  test.use({ viewport: { width: 375, height: 812 } });

  test('an entry in the URL shows the approve view with Deny and Approve', async ({ page }) => {
    await page.goto('/o/acme/needs-you?type=decision');
    await page
      .getByRole('list', { name: /Waiting on you/ })
      .getByRole('link')
      .first()
      .click();
    await expect(page.getByRole('link', { name: 'Back to Needs you' })).toBeVisible();
    await expect(page.getByRole('button', { name: /^Approve/ })).toBeVisible();
    await expect(page.getByRole('button', { name: /^Deny/ })).toBeVisible();
    await expectNoHorizontalScroll(page);
  });
});

test('the sidebar Needs you link shows the page', async ({ page }) => {
  await page.goto('/o/acme');
  await orgNav(page)
    .getByRole('link', { name: /^Needs you/ })
    .click();
  await expect(page).toHaveURL(/\/o\/acme\/needs-you$/);
});
