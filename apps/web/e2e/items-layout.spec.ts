// The Items table at desktop widths: every column shows, long titles end in an ellipsis.
// Not run in the box (no browser): CI runs it with the mock server.
import { expect, test } from '@playwright/test';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

for (const { width, height } of [
  { width: 1280, height: 900 },
  { width: 1440, height: 1000 },
]) {
  test(`shows every column without sideways scroll at ${width} px`, async ({ page }) => {
    await page.setViewportSize({ width, height });
    await page.goto('/o/acme/f/goal/items');

    const table = page.getByRole('table', { name: 'Items' });
    await expect(table.getByRole('columnheader', { name: 'Cost' })).toBeVisible();
    const fits = await table.evaluate((t) => {
      const box = t.parentElement as HTMLElement;
      return t.scrollWidth <= box.clientWidth && box.scrollWidth <= box.clientWidth;
    });
    expect(fits).toBe(true);
  });
}
