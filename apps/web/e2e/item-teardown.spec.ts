// The item's Teardown tab (ADR 0019, decision 3): a finished item with a failed check and human touches (item 4),
// an item no agent picked up (item 15), a failed read, and the link into the Trace tab. Not run in the box (no browser).
import { expect, test } from '@playwright/test';
import { expectNoHorizontalScroll } from './support';

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
  await request.post('/__mock/fail', { data: { path: '/teardown', method: 'GET', status: 503, times: 1 } });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/o/acme/f/goal/items/4?tab=teardown');

  await expect(page.getByRole('alert')).toBeVisible();
  await page.getByRole('button', { name: 'Try again' }).click();
  await expect(page.getByRole('region', { name: 'Totals' })).toBeVisible();
});
