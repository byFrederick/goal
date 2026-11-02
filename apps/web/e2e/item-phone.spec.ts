// The item page on a phone (ADR 0016, screens/mobile/item.jpg): the top bar with Back to Board and the More
// actions menu, the summary line under the stepper, Latest in this run, the one Details card and the note
// field above the tab bar. Runs on the phone-375 project; checked in light and dark mode.
import { expect, test } from '@playwright/test';
import { expectNoHorizontalScroll, isPhone } from './support';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

for (const scheme of ['light', 'dark'] as const) {
  test(`the item page on a phone shows its phone parts in ${scheme} mode`, async ({ page }) => {
    test.skip(!isPhone(page), 'the phone layout only');
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto('/o/acme/f/goal/items/12');

    const bar = page.getByRole('banner');
    await expect(bar.getByRole('link', { name: 'Back to Board' })).toBeVisible();
    await expect(bar.getByRole('button', { name: 'More actions' })).toBeVisible();
    await expect(page.getByText(/ in the line/)).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Latest in this run' })).toBeVisible();
    await expect(page.getByRole('button', { name: /^All \d+ events$/ })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Details' })).toBeVisible();
    await expect(page.getByRole('textbox', { name: 'Note for the agent' }).filter({ visible: true })).toBeVisible();
    await expectNoHorizontalScroll(page);
  });
}

for (const width of [375, 390]) {
  test(`the item page does not scroll sideways at ${width}`, async ({ page }) => {
    await page.setViewportSize({ width, height: 800 });
    await page.goto('/o/acme/f/goal/items/12');
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  });
}

for (const theme of ['light', 'dark'] as const) {
  test(`item page screenshot at 390, ${theme}`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.addInitScript((value) => window.localStorage.setItem('goal-theme', value), theme);
    await page.goto('/o/acme/f/goal/items/12');
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
    await testInfo.attach(`item-390-${theme}`, {
      body: await page.screenshot({ fullPage: true }),
      contentType: 'image/png',
    });
  });
}
