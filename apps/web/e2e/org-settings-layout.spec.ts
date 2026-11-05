// The organization's Code hosts (canvas organization/org-settings) against the mock: at 768 and 390 px, in
// light and dark, nothing goes past the viewport edge, no text is clipped, and a phone's tap targets are
// 44 px. These specs are not run in the box (no browser); CI runs them.
import { expect, test } from '@playwright/test';
import { expectNoClippedElements, expectNoHorizontalScroll } from './support';
import { expectTarget44 } from './support-boxes';

const sizes = [
  { width: 768, height: 1024 },
  { width: 390, height: 844 },
];

const path = '/o/acme/settings';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

for (const size of sizes) {
  for (const scheme of ['light', 'dark'] as const) {
    test(`Code hosts fit ${size.width} px in ${scheme}`, async ({ page }, testInfo) => {
      test.skip(testInfo.project.name !== 'desktop-1440', 'one project runs the whole matrix');
      await page.setViewportSize(size);
      await page.emulateMedia({ colorScheme: scheme });
      await page.goto(path);
      const hosts = page.getByRole('region', { name: 'Code hosts' });
      await expect(hosts.getByText('github.com')).toBeVisible();

      await expectNoHorizontalScroll(page);
      await expectNoClippedElements(page);
      await testInfo.attach(`org-code-hosts-${size.width}-${scheme}`, {
        body: await page.screenshot(),
        contentType: 'image/png',
      });
    });
  }
}

test('the Code hosts ⓘ is 44 px on a phone and its tip stays inside the viewport', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop-1440', 'one project runs the whole matrix');
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(path);
  const hosts = page.getByRole('region', { name: 'Code hosts' });
  const tip = hosts.getByRole('button', { name: 'About Code hosts' });
  await expectTarget44(tip, 'About Code hosts');
  await tip.click();
  const box = await page.getByRole('tooltip').first().boundingBox();
  expect(box).not.toBeNull();
  expect(box?.x ?? -1).toBeGreaterThanOrEqual(0);
  expect((box?.x ?? 0) + (box?.width ?? 0)).toBeLessThanOrEqual(390);
});
