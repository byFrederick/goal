// An agent's page (canvas build/agent) against the mock (the Settings aside shows an Environment select): at 1440, 1280, 1024, 768
// and 390 px, in light and dark, nothing goes past the viewport edge, no text is clipped, and a phone's
// tap targets are 44 px. These specs are not run in the box (no browser); CI runs them.
import { expect, test } from '@playwright/test';
import { expectNoClippedElements, expectNoHorizontalScroll } from './support';

const sizes = [
  { width: 1440, height: 1000 },
  { width: 1280, height: 900 },
  { width: 1024, height: 900 },
  { width: 768, height: 1024 },
  { width: 390, height: 844 },
];

const path = '/o/acme/f/goal/agents/reviewer';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

for (const size of sizes) {
  for (const scheme of ['light', 'dark'] as const) {
    test(`fits ${size.width} px in ${scheme}`, async ({ page }, testInfo) => {
      test.skip(testInfo.project.name !== 'desktop-1440', 'one project runs the whole matrix');
      await page.setViewportSize(size);
      await page.emulateMedia({ colorScheme: scheme });
      await page.goto(path);
      await expect(page.getByRole('region', { name: 'Settings' })).toBeVisible();

      await expectNoHorizontalScroll(page);
      await expectNoClippedElements(page);
      await testInfo.attach(`agent-${size.width}-${scheme}`, {
        body: await page.screenshot(),
        contentType: 'image/png',
      });
    });
  }
}

test('keeps the 44 px tap targets on a phone', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop-1440', 'one project runs the whole matrix');
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(path);
  await expect(page.getByRole('region', { name: 'Settings' })).toBeVisible();
  const targets = page
    .getByRole('main')
    .locator('a[href]:not(p a, dd a), button, input:not([type=hidden]), [role=combobox]');
  const boxes = await targets.evaluateAll((els) =>
    els.map((el) => {
      const { width, height } = el.getBoundingClientRect();
      return { name: (el.getAttribute('aria-label') ?? el.textContent ?? '').trim().slice(0, 30), width, height };
    }),
  );
  for (const box of boxes.filter((b) => b.width > 8 && b.height > 8)) {
    expect(box.height, `${box.name} is ${box.height}px tall`).toBeGreaterThanOrEqual(44);
  }
});

for (const width of [375, 768, 1440]) {
  test(`the selected tab is inside the viewport at ${width} px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.goto(path);
    const tab = page.getByRole('navigation').getByRole('link', { name: 'Overview' }).first();
    const box = await tab.boundingBox();
    expect(box && box.x >= 0 && box.x + box.width <= width).toBe(true);
  });
}
