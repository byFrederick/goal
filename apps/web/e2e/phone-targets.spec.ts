// Phone targets (MASTER.md, Phones): below 768 px every target answers across at least 44 px. Five screens
// the 390 px sweep found short: Home's links, the organization address, the Floor's Decide, a secret's
// knowledge source and an Items row title.
import { test } from '@playwright/test';
import { expectTarget44 } from './support-boxes';

test.use({ viewport: { width: 390, height: 844 } });

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

for (const colorScheme of ['light', 'dark'] as const) {
  test.describe(colorScheme, () => {
    test.beforeEach(async ({ page }) => {
      await page.emulateMedia({ colorScheme });
    });

    test('Home links are 44 px', async ({ page }) => {
      // The seed has no archived factory, and the link shows only with one: archive one in the answer.
      await page.route(/\/factories\?archived=true/, async (route) => {
        const response = await route.fetch({ url: route.request().url().replace('archived=true', 'archived=false') });
        const { factories } = await response.json();
        const [first] = factories;
        await route.fulfill({ json: { factories: [{ ...first, archived_at: '2026-10-25T00:00:00Z' }] } });
      });
      await page.goto('/o/acme');
      const main = page.getByRole('main');
      await expectTarget44(main.getByRole('link', { name: 'Archived factories' }), 'Archived factories');
      await expectTarget44(main.getByRole('link', { name: 'Open all' }), 'Open all');
      await expectTarget44(main.getByRole('link', { name: 'Spend and billing' }), 'Spend and billing');
    });

    test('the organization address input is 44 px', async ({ page }) => {
      await page.goto('/o/acme/settings');
      await expectTarget44(page.locator('#oslug'), 'Address');
    });

    test('the Floor Decide link is 44 px', async ({ page }) => {
      await page.goto('/o/acme/f/sre');
      await expectTarget44(page.getByRole('link', { name: 'Decide' }).first(), 'Decide');
    });

    test("a secret's knowledge source link is 44 px", async ({ page }) => {
      await page.goto('/o/acme/secrets/github-token');
      await expectTarget44(page.getByRole('link', { name: 'goal-docs', exact: true }), 'goal-docs');
    });

    test('the first Items row title link is 44 px', async ({ page }) => {
      await page.goto('/o/acme/f/goal/items');
      const row = page.getByRole('table', { name: 'Items' }).getByRole('row').nth(1);
      await expectTarget44(row.getByRole('link').first(), 'Items row title');
    });
  });
}
