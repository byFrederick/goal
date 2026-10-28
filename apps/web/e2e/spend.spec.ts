// Spend and billing (organization/spend.html) against the mock, at 375, 768 and 1440 px: the window
// control is kept in the URL, a budget row shows, Export CSV downloads the window's CSV, and the sidebar's
// "Spend this week" card links here. Not run in the box (no browser here): see the pull request.
import { expect, type Page, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll, isPhone } from './support';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

const heading = (page: Page) => page.getByRole('heading', { level: 1, name: 'Spend and billing' });
const windowControl = (page: Page) => page.getByRole('radio', { name: 'This week' });

for (const width of [375, 768, 1440]) {
  test.describe(`at ${width} px`, () => {
    test.use({ viewport: { width, height: 900 } });

    test('shows the screen, its figures and its budgets', async ({ page }) => {
      await page.goto('/o/acme/spend');
      await expect(heading(page)).toBeVisible();
      await expect(page).toHaveTitle('Spend and billing · Acme · Goal');
      await expect(windowControl(page)).toBeChecked();
      await expect(page.getByRole('region', { name: 'Spend this week' })).toBeVisible();

      // One budget row: Software, with its daily and weekly caps.
      const budgets = page.getByRole('heading', { level: 2, name: 'Budgets' }).locator('xpath=ancestor::section[1]');
      await expect(budgets.getByText('Software', { exact: true })).toBeVisible();
      await expect(budgets.getByText('$150 a day, $800 a week')).toBeVisible();

      // Export CSV is built; the router's Set up is not built yet: disabled, never a live link or button.
      await expect(page.getByRole('button', { name: 'Export CSV' })).not.toHaveAttribute('aria-disabled', 'true');
      await expect(page.getByRole('button', { name: 'Set up' })).toHaveAttribute('aria-disabled', 'true');

      await expectAccessible(page);
      await expectNoHorizontalScroll(page);
    });

    test('keeps the window in the URL', async ({ page }) => {
      await page.goto('/o/acme/spend');
      await page.getByRole('radio', { name: 'This month' }).check();
      await expect(page).toHaveURL(/[?&]window=month(&|$)/);
      await expect(page.getByRole('radio', { name: 'This month' })).toBeChecked();

      await page.getByRole('radio', { name: 'Last 90 days' }).check();
      await expect(page).toHaveURL(/[?&]window=90d(&|$)/);
    });

    test('is linked from the sidebar, or the rail on a laptop', async ({ page }) => {
      await page.goto('/o/acme');
      if (isPhone(page)) test.skip(true, 'the sidebar sits in a drawer on phones');
      await page
        .getByRole('link', { name: /Spend this week/ })
        .first()
        .click();
      await expect(page).toHaveURL(/\/o\/acme\/spend$/);
      await expect(heading(page)).toBeVisible();
    });
  });
}
