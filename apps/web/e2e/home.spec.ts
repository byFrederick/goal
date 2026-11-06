// Home (/o/acme) against the mock: the compact line, Needs you first and Spend this week, screenshot at 1440 and
// 390 in both themes. Not run in the box (no browser): CI runs it with the mock server.
import { expect, test } from '@playwright/test';
import { expectNoHorizontalScroll, pinClock } from './support';

test.beforeEach(async ({ page, request }) => {
  await pinClock(page, request);
});

const sizes = [
  { name: '1440', width: 1440, height: 900 },
  { name: '1280', width: 1280, height: 800 },
  { name: '1024', width: 1024, height: 768 },
  { name: '768', width: 768, height: 900 },
  { name: '390', width: 390, height: 844 },
] as const;

for (const scheme of ['light', 'dark'] as const) {
  for (const size of sizes) {
    test(`Home at ${size.name} in ${scheme} mode`, async ({ page }, testInfo) => {
      await page.setViewportSize({ width: size.width, height: size.height });
      await page.emulateMedia({ colorScheme: scheme });
      await page.goto('/o/acme');

      await expect(page.getByRole('heading', { level: 1, name: 'Home' })).toBeVisible();
      await expect(page.getByRole('region', { name: 'Needs you first' })).toBeVisible();
      await expect(page.getByRole('region', { name: 'Spend this week' }).getByText('Last 7 days')).toBeVisible();
      await expect(page.getByRole('link', { name: 'Archived factories' })).toBeVisible();
      await expect(page.getByRole('radio', { name: 'Archived' })).toHaveCount(0);
      await expectNoHorizontalScroll(page);
      if (size.width >= 768)
        await expect(page.getByRole('banner').getByRole('button', { name: /New item/ })).toBeInViewport();

      await testInfo.attach(`home-${size.name}-${scheme}`, {
        body: await page.screenshot({ fullPage: true }),
        contentType: 'image/png',
      });
    });
  }
}
