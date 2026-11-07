// The sidebar names a factory by its title ("Software"), never its slug ("software"), at a phone and a desktop width.
// Not run in the box: Playwright needs a browser, which the box does not have.
import { expect, test } from '@playwright/test';
import { openNavigation, orgNav } from './support';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

for (const width of [375, 1440]) {
  test(`the sidebar's factory list reads the title at ${width} px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/o/acme');
    await openNavigation(page);
    const nav = orgNav(page).filter({ visible: true });
    await expect(nav.getByText('Software', { exact: true })).toBeVisible();
    await expect(nav.getByText('software', { exact: true })).toHaveCount(0);
  });
}
