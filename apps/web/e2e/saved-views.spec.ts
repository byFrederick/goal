// Saved views on the Items page (ADR 0020) against the mock: a person saves the page's filters as a
// view, opens it from the nav, renames it and deletes it. The mock starts with no views. Not run in the
// box (no browser here): see the pull request.
import { expect, type Page, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll } from './support';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

const nav = (page: Page) => page.getByRole('navigation', { name: 'Saved views' });

for (const width of [375, 768, 1440]) {
  test.describe(`at ${width} px`, () => {
    test.use({ viewport: { width, height: 900 } });

    test('saves the current filters, opens, renames and deletes a view', async ({ page }) => {
      await page.goto('/o/acme/f/goal/items?state=waiting_human');

      // The built-in tabs are there and "+ Save view" is a live button, not "Not available yet".
      await expect(nav(page).getByRole('link', { name: /^Waiting on you/ })).toHaveAttribute('aria-current', 'page');
      await nav(page).getByRole('button', { name: '+ Save view' }).click();
      const save = page.getByRole('dialog', { name: 'Save view' });
      await save.getByRole('textbox', { name: 'Name' }).fill('Needs me');
      await save.getByRole('button', { name: 'Save view' }).click();
      await expect(page.getByRole('dialog', { name: 'Save view' })).toHaveCount(0);

      // The new view is in the nav, and it links back to the filters it saved.
      const view = nav(page).getByRole('link', { name: 'Needs me' });
      await expect(view).toHaveAttribute('href', /state=waiting_human/);

      // Rename it through its menu; the name changes, the filters stay.
      await nav(page).getByRole('button', { name: 'Actions for Needs me' }).click();
      await page.getByRole('menuitem', { name: 'Rename…' }).click();
      const rename = page.getByRole('dialog', { name: 'Rename view' });
      await rename.getByRole('textbox', { name: 'Name' }).fill('Waiting');
      await rename.getByRole('button', { name: 'Rename' }).click();
      await expect(nav(page).getByRole('link', { name: 'Waiting', exact: true })).toBeVisible();

      // Delete it after the person confirms.
      await nav(page).getByRole('button', { name: 'Actions for Waiting' }).click();
      await page.getByRole('menuitem', { name: 'Delete…' }).click();
      await page
        .getByRole('dialog', { name: /^Delete/ })
        .getByRole('button', { name: 'Delete' })
        .click();
      await expect(nav(page).getByRole('link', { name: 'Waiting', exact: true })).toHaveCount(0);

      await expectAccessible(page);
      await expectNoHorizontalScroll(page);
    });
  });
}
