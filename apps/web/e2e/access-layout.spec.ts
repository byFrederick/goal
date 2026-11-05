// Access against the mock (canvas connections/access, access-768, mobile/access): at 1440, 1280, 1024, 768
// and 390 px, in light and dark, nothing goes past the viewport edge, the matrix shows from 1000 px up and
// the list below it, and a phone's tap targets are 44 px. These specs are not run in the box (no browser);
// CI runs them.
import { expect, test } from '@playwright/test';
import { expectAccessible, expectNoClippedElements, expectNoHorizontalScroll } from './support';

const sizes = [
  { width: 1440, height: 1000 },
  { width: 1280, height: 900 },
  { width: 1024, height: 900 },
  { width: 768, height: 1024 },
  { width: 390, height: 844 },
];

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

for (const size of sizes) {
  for (const scheme of ['light', 'dark'] as const) {
    test(`fits ${size.width} px in ${scheme}, with the matrix from 1000 px and the list below`, async ({
      page,
    }, testInfo) => {
      test.skip(testInfo.project.name !== 'desktop-1440', 'one project runs the whole matrix');
      await page.setViewportSize(size);
      await page.emulateMedia({ colorScheme: scheme });
      await page.goto('/o/acme/access');
      await expect(page.getByRole('region', { name: 'Expiring' })).toBeVisible();

      await expectNoHorizontalScroll(page);
      await expectNoClippedElements(page);
      const matrix = page.getByRole('table');
      const list = page.getByRole('region', { name: 'MCP servers' });
      if (size.width >= 1000) {
        await expect(matrix).toBeVisible();
        await expect(list).toBeHidden();
      } else {
        await expect(matrix).toBeHidden();
        await expect(list).toBeVisible();
      }
      await testInfo.attach(`access-${size.width}-${scheme}`, {
        body: await page.screenshot(),
        contentType: 'image/png',
      });
    });
  }
}

test('keeps the 44 px tap targets on a phone', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop-1440', 'one project runs the whole matrix');
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/o/acme/access');
  const targets = page
    .getByRole('radio')
    .locator('xpath=..')
    .or(page.getByRole('region', { name: 'MCP servers' }).getByRole('link'))
    .or(page.getByRole('link', { name: 'Replace value' }))
    .or(page.getByRole('region', { name: 'MCP servers' }).locator('a[href*="/secrets/"]'));
  await expect(targets.first()).toBeVisible();
  const boxes = await targets.evaluateAll((els) =>
    els.map((el) => {
      const { width, height } = el.getBoundingClientRect();
      return { name: el.textContent?.trim().slice(0, 30) ?? '', width, height };
    }),
  );
  for (const box of boxes.filter((b) => b.width > 0)) {
    expect(box.height, `${box.name} is ${box.height}px tall`).toBeGreaterThanOrEqual(44);
  }
  await expectAccessible(page);
});

test('switches the view and opens an environment’s file from a row', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop-1440', 'the 1440 project');
  await page.goto('/o/acme/access');
  await page.getByRole('radio', { name: 'By factory' }).click();
  await expect(page).toHaveURL(/by=factory/);
  await page.getByRole('radio', { name: 'By what they reach' }).click();
  await page.getByRole('table').getByRole('link', { name: 'CLOUDFLARE_API_TOKEN', exact: true }).click();
  await expect(page).toHaveURL(
    /\/o\/acme\/f\/software\/definition\/basics\?mode=yaml&file=environments%2Fdeploy\.yaml/,
  );
});

for (const width of [1280, 768]) {
  for (const path of ['/o/acme/access', '/o/acme/access?by=secret']) {
    test(`secret chips are at least 24 px tall at ${width} px on ${path}`, async ({ page }, testInfo) => {
      test.skip(testInfo.project.name !== 'desktop-1440', 'one project runs the whole matrix');
      await page.setViewportSize({ width, height: 900 });
      await page.goto(path);
      await expect(page.getByRole('region', { name: 'Expiring' })).toBeVisible();
      const chips = page.locator('a[href*="/secrets/"][translate="no"]:visible');
      await expect(chips.first()).toBeVisible();
      const heights = await chips.evaluateAll((els) => els.map((el) => el.getBoundingClientRect().height));
      for (const h of heights) expect(h).toBeGreaterThanOrEqual(24);
      await expectAccessible(page);
    });
  }
}
