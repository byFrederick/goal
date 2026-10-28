// The desktop Board: every column's body starts at the same height, whatever it holds (an empty column,
// a column of cards, the drum's labelled column), and the header cards share one height.
import { expect, test } from '@playwright/test';
import { isPhone } from './support';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

/**
 * The top edge of each column's first body element (a card, or the Empty text), and its header's height.
 * All boxes are read in one page.evaluate, so they come from one layout pass: reading them one await at a
 * time lets the board move between reads (the mock's latency shifts it once the columns have loaded).
 */
async function columnEdges(page: import('@playwright/test').Page) {
  return page.evaluate(() => {
    const columns = Array.from(
      document.querySelectorAll<HTMLElement>('[role="region"][aria-label="Stations"] > div > section'),
    );
    const tops: { name: string; top: number }[] = [];
    const heights: { name: string; height: number }[] = [];
    for (const column of columns) {
      const name = column.getAttribute('aria-label') ?? 'column';
      const header = column.querySelector<HTMLElement>(':scope > header');
      const zone = column.querySelector<HTMLElement>(':scope > div:last-of-type');
      const first = zone?.querySelector<HTMLElement>(':scope > article, :scope > p');
      if (!header || !first) throw new Error(`${name}: no header or first body element is laid out`);
      tops.push({ name, top: first.getBoundingClientRect().top });
      heights.push({ name, height: header.getBoundingClientRect().height });
    }
    return { tops, heights };
  });
}

test('the first card or Empty text of every desktop column is level, and the headers match', async ({ page }) => {
  test.skip(isPhone(page), 'the stations are side by side from 768 px');
  await page.goto('/o/acme/f/goal/board');
  await expect(page.getByRole('region', { name: 'Stations' })).toBeVisible();
  // Let the board finish loading before measuring: its first render is shifted by the mock's latency.
  await page.waitForLoadState('networkidle');

  const { tops, heights } = await columnEdges(page);
  expect(tops.length).toBeGreaterThan(1);
  const topValues = tops.map((t) => t.top);
  expect(Math.max(...topValues) - Math.min(...topValues), JSON.stringify(tops)).toBeLessThanOrEqual(1);
  const headerValues = heights.map((h) => h.height);
  expect(Math.max(...headerValues) - Math.min(...headerValues), JSON.stringify(heights)).toBeLessThanOrEqual(1);
});

test('on a phone the empty sub-header slot is not shown', async ({ page }) => {
  test.skip(!isPhone(page), 'the phone shows one column at a time');
  await page.goto('/o/acme/f/goal/board');
  // The phone's columns are behind the station tabs; the slot is hidden at every width below 768 px.
  const slots = page.locator('[aria-hidden="true"].min-h-\\[1lh\\]');
  for (let i = 0; i < (await slots.count()); i++) {
    await expect(slots.nth(i)).toBeHidden();
  }
});
