// The item's Timeline tab at 1440 px (docs/design/screens/daily-work/item.html, the Build run card): every event
// row fits inside its run card, reads as a sentence whose middle column keeps its width, a long command stays on
// one line, and a failed step's raw output waits behind its disclosure until it is opened.
import { expect, test } from '@playwright/test';
import { expectNoHorizontalScroll } from './support';

test.use({ viewport: { width: 1440, height: 900 } });

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

test('the event rows of a run fit its card and keep raw output behind a disclosure', async ({ page }) => {
  await page.goto('/o/acme/f/goal/items/343');

  const events = page.getByRole('list', { name: /^Events by/ });
  const rows = events.getByRole('listitem');
  await expect(rows).toHaveCount(4);
  await expect(rows.filter({ hasText: 'Read 2 files in apps/web/src/features/items' })).toHaveCount(1);

  // Each row's middle cell is the row's own div child (after the time and the icon tile).
  const card = page.locator('article').filter({ has: events });
  const cardBox = await card.boundingBox();
  const measured = await rows.evaluateAll((els) =>
    els.map((row) => {
      const middle = row.querySelector(':scope > div') as HTMLElement;
      const rect = row.getBoundingClientRect();
      return {
        text: row.textContent ?? '',
        scrollWidth: row.scrollWidth,
        clientWidth: row.clientWidth,
        left: rect.left,
        right: rect.right,
        middleWidth: middle.getBoundingClientRect().width,
        middleHeight: middle.getBoundingClientRect().height,
        lineHeight: Number.parseFloat(getComputedStyle(middle).lineHeight),
      };
    }),
  );

  expect(cardBox).not.toBeNull();
  for (const row of measured) {
    expect(row.scrollWidth, row.text).toBeLessThanOrEqual(row.clientWidth);
    expect(row.left, row.text).toBeGreaterThanOrEqual(cardBox?.x ?? 0);
    expect(row.right, row.text).toBeLessThanOrEqual((cardBox?.x ?? 0) + (cardBox?.width ?? 0));
    expect(row.middleWidth, row.text).toBeGreaterThanOrEqual(240);
  }

  const longCommand = measured.find((row) => row.text.includes('Ran go test'));
  expect(longCommand).toBeDefined();
  expect(longCommand?.middleHeight).toBeGreaterThanOrEqual(longCommand?.lineHeight ?? 0);

  // Every time stamp is one line, and the item page has the timeline left of the details.
  const times = await events.locator('time').evaluateAll((els) =>
    els.map((el) => ({
      text: el.textContent ?? '',
      height: el.getBoundingClientRect().height,
      line: Number.parseFloat(getComputedStyle(el).lineHeight),
    })),
  );
  for (const t of times) expect(t.height, t.text).toBeLessThanOrEqual(t.line + 1);
  const timelineBox = await events.boundingBox();
  const detailsBox = await page.getByRole('heading', { name: 'Details' }).boundingBox();
  expect(timelineBox?.x ?? 0).toBeLessThan(detailsBox?.x ?? 0);

  const failed = rows.filter({ hasText: 'Ran pnpm' });
  await expect(failed).toContainText('failed');
  await expect(page.locator('pre').filter({ hasText: 'output without a break' })).toBeHidden();
  await failed.getByText('Show the output').click();
  await expect(page.locator('pre').filter({ hasText: 'output without a break' })).toBeVisible();

  await expectNoHorizontalScroll(page);
});

for (const theme of ['light', 'dark'] as const) {
  test(`item timeline screenshot at 1440, ${theme}`, async ({ page }, testInfo) => {
    await page.addInitScript((value) => window.localStorage.setItem('goal-theme', value), theme);
    await page.goto('/o/acme/f/goal/items/343');
    await expect(page.getByRole('tab', { name: 'Timeline' })).toHaveAttribute('aria-selected', 'true');
    await testInfo.attach(`item-1440-${theme}`, {
      body: await page.screenshot({ fullPage: true }),
      contentType: 'image/png',
    });
  });
}
