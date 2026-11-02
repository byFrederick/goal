// Items against the mock: every column shows at 1440, the view tabs stay on one line at 768, and the
// pictures for the pull request.
import { expect, test } from '@playwright/test';
import { expectNoHorizontalScroll } from './support';

const items = '/o/acme/f/goal/items';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

test.describe('desktop', () => {
  test('shows State, Agent, Age and Cost inside the table box at 1440', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'desktop-1440', 'the 1440 project');
    await page.goto(items);
    const table = page.getByRole('table', { name: 'Items' });
    await expect(table).toBeVisible();
    const box = await table.locator('xpath=..').boundingBox();
    for (const name of ['State', 'Agent', 'Age', 'Cost']) {
      const header = table.getByRole('columnheader', { name, exact: true });
      await expect(header).toBeVisible();
      const at = await header.boundingBox();
      expect(at && box && at.x + at.width <= box.x + box.width + 1).toBe(true);
    }
    const scrolls = await table.locator('xpath=..').evaluate((el) => el.scrollWidth - el.clientWidth);
    expect(scrolls).toBeLessThanOrEqual(0);
  });
});

test('keeps the view tabs on one line at 768', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'tablet-768', 'the 768 project');
  await page.goto(items);
  const nav = page.getByRole('navigation', { name: 'Saved views' });
  await expect(nav).toBeVisible();
  const tabs = nav.getByRole('link').or(nav.getByRole('button', { name: '+ Save view' }));
  const boxes = await tabs.evaluateAll((els) => els.map((el) => el.getBoundingClientRect()));
  expect(boxes.length).toBeGreaterThan(3);
  expect(new Set(boxes.map((b) => Math.round(b.top))).size).toBe(1);
  expect(new Set(boxes.map((b) => Math.round(b.height))).size).toBe(1);
  await expectNoHorizontalScroll(page);
});

// Attached to the report, not compared; the design exports are the reference.
for (const theme of ['light', 'dark'] as const) {
  test.describe(`screenshots, ${theme}`, () => {
    test(`items at the project's width, ${theme}`, async ({ page }, testInfo) => {
      test.skip(testInfo.project.name !== 'desktop-1440', 'the 1440 project');
      await page.addInitScript((value) => window.localStorage.setItem('goal-theme', value), theme);
      await page.goto(items);
      await expect(page.getByRole('heading', { level: 1, name: 'Items' })).toBeVisible();
      await testInfo.attach(`items-1440-${theme}`, {
        body: await page.screenshot({ fullPage: true }),
        contentType: 'image/png',
      });
    });

    test.describe('phone', () => {
      test.use({ viewport: { width: 390, height: 844 } });
      test(`items at 390, ${theme}`, async ({ page }, testInfo) => {
        test.skip(testInfo.project.name !== 'phone-375', 'the phone project');
        await page.addInitScript((value) => window.localStorage.setItem('goal-theme', value), theme);
        await page.goto(items);
        await expect(page.getByRole('heading', { level: 1, name: 'Items' })).toBeVisible();
        await testInfo.attach(`items-390-${theme}`, {
          body: await page.screenshot({ fullPage: true }),
          contentType: 'image/png',
        });
      });
    });
  });
}
