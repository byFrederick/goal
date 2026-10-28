// Search from the sidebar: ⌘K (Ctrl+K off macOS) or the Search icon opens it, "spend" finds the mock's
// items across the organization's factories, the arrow keys move the highlight and Enter opens a row.
import { expect, test } from '@playwright/test';
import { expectAccessible, orgNav, recordToasts } from './support';

test.beforeEach(async ({ page, request }) => {
  await request.post('/__mock/reset');
  await recordToasts(page);
});

for (const width of [768, 1440]) {
  test.describe(`at ${width} px`, () => {
    test.use({ viewport: { width, height: 900 } });

    test('⌘K opens search, "spend" lists the items, the arrow keys and Enter open one', async ({ page }) => {
      await page.goto('/o/acme/f/goal');
      await expect(orgNav(page).getByRole('button', { name: 'Search' })).toBeVisible();

      await page.keyboard.press('ControlOrMeta+k');
      const input = orgNav(page).getByRole('combobox', { name: 'Search' });
      await expect(input).toBeFocused();
      await input.fill('spend');

      const options = orgNav(page).getByRole('option');
      await expect(options.filter({ hasText: "Show this week's spend" })).toHaveCount(1);
      await expect(options.filter({ hasText: 'Check the spend against the budget' })).toHaveCount(1);
      await expect(options.filter({ hasText: 'S3 spend up 22% month over month' })).toHaveCount(1);
      await expect(options.first()).toHaveAttribute('aria-selected', 'true');
      await expectAccessible(page);

      await page.keyboard.press('ArrowDown');
      const second = options.nth(1);
      await expect(second).toHaveAttribute('aria-selected', 'true');
      await expect(input).toHaveAttribute('aria-activedescendant', (await second.getAttribute('id')) ?? '');

      await page.keyboard.press('Enter');
      await expect(page).toHaveURL(/\/o\/acme\/f\/[^/]+\/items\/\d+$/);
      await expect(orgNav(page).getByRole('combobox', { name: 'Search' })).toBeHidden();
    });

    test('Escape closes search and returns focus to the Search icon', async ({ page }) => {
      await page.goto('/o/acme/f/goal');
      await orgNav(page).getByRole('button', { name: 'Search' }).click();

      await page.keyboard.press('Escape');
      await expect(orgNav(page).getByRole('combobox', { name: 'Search' })).toBeHidden();
      await expect(orgNav(page).getByRole('button', { name: 'Search' })).toBeFocused();
    });

    test('"spend" offers New item about it in Actions, and Enter opens the composer with the title', async ({
      page,
    }) => {
      await page.goto('/o/acme/f/goal');
      await orgNav(page).getByRole('button', { name: 'Search' }).click();
      await orgNav(page).getByRole('combobox', { name: 'Search' }).fill('spend');

      const newItem = orgNav(page).getByRole('option', { name: 'New item about "spend"' });
      await expect(newItem).toBeVisible();
      for (let i = 0; i < 10 && (await newItem.getAttribute('aria-selected')) !== 'true'; i++) {
        await page.keyboard.press('ArrowDown');
      }
      await expect(newItem).toHaveAttribute('aria-selected', 'true');

      await page.keyboard.press('Enter');
      const dialog = page.getByRole('dialog', { name: 'New item' });
      await expect(dialog).toBeVisible();
      await expect(dialog.getByLabel('Title')).toHaveValue('spend');
    });
  });
}
