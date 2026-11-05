// The item's Teardown tab (ADR 0019, decision 3): a finished item with a failed check and human touches (item 4),
// an item no agent picked up (item 15), a failed read, and the link into the Trace tab. Not run in the box (no browser).
import { expect, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll } from './support';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

test('the Teardown tab shows where a finished item went', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/o/acme/f/goal/items/4');
  await page.getByRole('tab', { name: 'Teardown' }).click();

  await expect(page).toHaveURL(/tab=teardown/);
  const panel = page.getByRole('tabpanel', { name: 'Teardown' });
  await expect(panel.getByRole('region', { name: 'Totals' })).toBeVisible();
  await expect(panel.getByRole('table', { name: 'Time by station' })).toBeVisible();
  await expect(panel.getByRole('table', { name: 'Runs' })).toBeVisible();
  await expect(panel.getByRole('heading', { name: 'Why it went back' })).toBeVisible();
  await expect(panel.getByRole('heading', { name: 'Human touches' })).toBeVisible();
  await expect(panel.getByText('Worked out from the item’s runs and events each time you open it.')).toBeVisible();
  await expectNoHorizontalScroll(page);
});

test('below 1000 px the Waiting column goes and Runs is a list', async ({ page }) => {
  await page.setViewportSize({ width: 768, height: 900 });
  await page.goto('/o/acme/f/goal/items/4?tab=teardown');

  const panel = page.getByRole('tabpanel', { name: 'Teardown' });
  await expect(panel.getByRole('columnheader', { name: 'Waiting' })).toBeHidden();
  await expect(panel.getByRole('table', { name: 'Runs' })).toBeHidden();
  await expect(panel.getByRole('list', { name: 'Runs' })).toBeVisible();
  await expectNoHorizontalScroll(page);
});

test('In the trace opens the Trace tab on the step that sent the item back', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/o/acme/f/goal/items/4?tab=teardown');
  await page.getByRole('tabpanel', { name: 'Teardown' }).getByRole('link', { name: 'In the trace' }).first().click();

  await expect(page).toHaveURL(/tab=trace&step=\d+/);
  await expect(page.getByRole('tabpanel', { name: 'Trace' }).getByRole('complementary')).toBeVisible();
});

test('an item no agent has picked up shows the empty state', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/o/acme/f/goal/items/15?tab=teardown');

  await expect(page.getByRole('heading', { name: 'No breakdown yet' })).toBeVisible();
});

test('a failed read shows an error, and Try again shows the breakdown', async ({ page, request }) => {
  // The query client tries a 5xx twice more (api/retry.ts), so the fault outlasts the first attempt and both retries.
  await request.post('/__mock/fail', { data: { path: '/teardown', method: 'GET', status: 503, times: 3 } });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/o/acme/f/goal/items/4?tab=teardown');

  // The two retries wait 1 s and 2 s before the error shows, more with the tablet project's latency.
  await expect(page.getByRole('alert')).toBeVisible({ timeout: 15_000 });
  await page.getByRole('button', { name: 'Try again' }).click();
  await expect(page.getByRole('region', { name: 'Totals' })).toBeVisible();
});

test('at 375 px no two station labels overlap and each has a title', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 800 });
  await page.goto('/o/acme/f/goal/items/4?tab=teardown');
  const labels = page.getByRole('table', { name: 'Time by station' }).locator('tbody th[scope="row"] [title]');
  await expect(labels.first()).toBeVisible();
  const count = await labels.count();
  expect(count).toBeGreaterThan(0);
  const boxes = [];
  for (let i = 0; i < count; i++) {
    expect(await labels.nth(i).getAttribute('title')).toBeTruthy();
    const b = await labels.nth(i).boundingBox();
    if (b) boxes.push(b);
  }
  for (let i = 0; i < boxes.length; i++) {
    for (let j = i + 1; j < boxes.length; j++) {
      const a = boxes[i];
      const b = boxes[j];
      if (!a || !b) continue;
      expect(a.y + a.height <= b.y || b.y + b.height <= a.y || a.x + a.width <= b.x || b.x + b.width <= a.x).toBe(true);
    }
  }
});

test('at 1440 px the runs scroller is focusable, named and accessible', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/o/acme/f/goal/items/4?tab=teardown');
  const region = page.getByRole('region', { name: 'Runs table', exact: true });
  await region.focus();
  await expect(region).toBeFocused();
  await expectAccessible(page);
});
