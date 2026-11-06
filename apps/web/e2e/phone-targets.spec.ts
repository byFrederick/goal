// Phone targets (MASTER.md, Phones): below 768 px every target answers across at least 44 px. Five screens
// the 390 px sweep found short: Home's links, the organization address, the Floor's Decide, a secret's
// knowledge source and an Items row title.
import { expect, test } from '@playwright/test';
import { boxOf, expectTarget44 } from './support-boxes';

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

    test('the first assembly Plan title link is 44 px tall', async ({ page }) => {
      await page.goto('/o/acme/f/goal/items/17');
      const row = page.getByRole('table', { name: 'Plan' }).getByRole('row').nth(1);
      const { height } = await boxOf(row.getByRole('link').first());
      expect(height, `Plan title link is ${height}px tall`).toBeGreaterThanOrEqual(44);
    });

    test('a Follow-ups to triage row is 44 px tall and opens its item from its centre', async ({ page }) => {
      await page.goto('/o/acme/f/software/intake');
      const row = page.getByRole('region', { name: 'Follow-ups to triage' }).getByRole('listitem').first();
      // The page is taller than the phone: a click at coordinates needs the row on screen.
      await row.scrollIntoViewIfNeeded();
      const box = await boxOf(row);
      expect(box.height, `triage row is ${box.height}px tall`).toBeGreaterThanOrEqual(44);
      await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
      await expect(page).toHaveURL(/\/items\//);
    });

    test('a Coming up row on Intake is 44 px tall', async ({ page }) => {
      await page.goto('/o/acme/f/software/intake');
      const row = page.getByRole('region', { name: 'Coming up' }).getByRole('listitem').first();
      const { height } = await boxOf(row);
      expect(height, `Coming up row is ${height}px tall`).toBeGreaterThanOrEqual(44);
    });

    test('a Board card is 44 px tall and opens its item from away from the title', async ({ page }) => {
      await page.goto('/o/acme/f/goal/board');
      // The first article in the DOM is a desktop column's card, hidden on a phone.
      const card = page.locator('article:visible').first();
      await card.scrollIntoViewIfNeeded();
      const box = await boxOf(card);
      expect(box.height, `Board card is ${box.height}px tall`).toBeGreaterThanOrEqual(44);
      // The middle of the left edge, inside the padding: a corner point falls outside the card's rounded
      // corner, and the Move button at the right is its own target.
      await page.mouse.click(box.x + 6, box.y + box.height / 2);
      await expect(page).toHaveURL(/\/items\//);
    });

    test('the wizard Visual and YAML buttons are 44 px tall', async ({ page }) => {
      await page.goto('/o/acme/new/start');
      for (const name of ['Visual', 'YAML']) {
        const { height } = await boxOf(page.getByRole('button', { name }));
        expect(height, `${name} is ${height}px tall`).toBeGreaterThanOrEqual(44);
      }
    });
  });
}
