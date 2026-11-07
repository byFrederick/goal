// Spend and billing (organization/spend.html) against the mock, at 375, 768 and 1440 px: the window
// control is kept in the URL, a budget row shows, Export CSV downloads the window's CSV, and the sidebar's
// "Spend this week" card links here. Not run in the box (no browser here): see the pull request.
import { expect, type Page, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll, isPhone, pinClock } from './support';

test.beforeEach(async ({ page, request }) => {
  await pinClock(page, request);
});

const heading = (page: Page) => page.getByRole('heading', { level: 1, name: 'Spend and billing' });
// The window control sits in the top bar from 768 px and in the page below it; Export CSV is always in the top bar.
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
      await expect(budgets.getByText('about $114 a day, $800 a week')).toBeVisible();
      await expect(budgets.getByText(/midnight/i)).toHaveCount(0);

      // Export CSV is built; the router's Set up is not built yet: disabled, never a live link or button.
      await expect(page.getByRole('button', { name: 'Export CSV' })).not.toHaveAttribute('aria-disabled', 'true');
      await expect(page.getByRole('button', { name: 'Set up' })).toHaveAttribute('aria-disabled', 'true');

      await expectAccessible(page);
      await expectNoHorizontalScroll(page);
    });

    test('a budget row agrees with Home and shows its own factory spend', async ({ page }) => {
      // SRE's weekly budget is $400 on both screens, so a day is about $57 on both.
      await page.goto('/o/acme');
      const home = page.getByRole('article').filter({ has: page.getByRole('heading', { name: 'SRE' }) });
      await expect(home.getByText('of $57.14')).toBeVisible();

      await page.goto('/o/acme/spend');
      const budgets = page.getByRole('heading', { level: 2, name: 'Budgets' }).locator('xpath=ancestor::section[1]');
      const row = (name: string) => budgets.getByRole('listitem').filter({ hasText: name });
      await expect(row('SRE').getByText('about $57 a day, $400 a week')).toBeVisible();
      await expect(row('SRE').getByText(/Today .* of \$57\.14|Today .* of \$57/)).toBeVisible();

      // Each row has its own 7-day figure, not the organization's total.
      const week = async (name: string) =>
        Number(
          (
            (await row(name)
              .getByText(/Last 7 days/)
              .textContent()) ?? ''
          )
            .match(/Last 7 days \$([\d,.]+)/)?.[1]
            ?.replace(/,/g, ''),
        );
      const [software, sre] = [await week('Software'), await week('SRE')];
      expect(sre).toBeGreaterThan(0);
      expect(sre).toBeLessThan(software);
    });

    test('the Today label stays inside its box', async ({ page }) => {
      await page.goto('/o/acme/spend');
      await expect(heading(page)).toBeVisible();
      const today = page.locator('[data-today] b').first();
      await expect(today).toHaveText('Today');
      const spills = await today.evaluate((el) => el.scrollWidth > el.clientWidth);
      expect(spills).toBe(false);
    });

    test('keeps the window in the URL', async ({ page }) => {
      await page.goto('/o/acme/spend');
      await page.getByRole('radio', { name: 'This month' }).click();
      await expect(page).toHaveURL(/[?&]window=month(&|$)/);
      await expect(page.getByRole('radio', { name: 'This month' })).toBeChecked();

      await page.getByRole('radio', { name: 'Last 90 days' }).click();
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

// Box usage (the CPU and memory of the window's runs by agent or station), at a phone's 390 px and a desktop's 1440 px.
for (const width of [390, 1440]) {
  test.describe(`Box usage at ${width} px`, () => {
    test.use({ viewport: { width, height: 900 } });

    test('shows the section, switches agents and stations and does not scroll sideways', async ({ page }) => {
      await page.goto('/o/acme/spend');
      const box = page.getByRole('region', { name: 'Box usage' });
      await expect(box).toBeVisible();
      await expect(box.getByRole('radio', { name: 'Agents' })).toBeChecked();
      await expect(box.getByText('backend-engineer')).toBeVisible();
      await expectNoHorizontalScroll(page);

      await box.getByRole('radio', { name: 'Stations' }).click();
      await expect(box.getByRole('radio', { name: 'Stations' })).toBeChecked();
      await expect(box.getByText('build').first()).toBeVisible();
      await expect(box.getByText('backend-engineer')).toHaveCount(0);
      await expectNoHorizontalScroll(page);
    });
  });
}
