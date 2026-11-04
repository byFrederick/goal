// The assembly page (ADR 0012, screens/daily-work/assembly.jpg): a live assembly (#17) and a finished one (#21)
// at 1440 and 390 px in both themes. A done assembly keeps its Progress, Parts and Plan and drops the actions.
// The pictures are attached to the report, not compared; the design exports are the reference.
import { expect, test } from '@playwright/test';
import { expectNoHorizontalScroll } from './support';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

const sizes = [
  { name: '1440', width: 1440, height: 900 },
  { name: '390', width: 390, height: 844 },
];
const assemblies = [
  { name: 'live', number: 17, done: false },
  { name: 'done', number: 21, done: true },
] as const;

for (const size of sizes) {
  for (const theme of ['light', 'dark'] as const) {
    for (const a of assemblies) {
      test(`the ${a.name} assembly at ${size.name}, ${theme}`, async ({ page }, testInfo) => {
        await page.setViewportSize({ width: size.width, height: size.height });
        await page.addInitScript((value) => window.localStorage.setItem('goal-theme', value), theme);
        await page.goto(`/o/acme/f/goal/items/${a.number}`);

        await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
        await expect(page.getByRole('heading', { name: 'Progress' })).toBeVisible();
        await expect(page.getByRole('heading', { name: 'Parts', exact: true })).toBeVisible();
        await expect(page.getByRole('heading', { name: 'Plan' })).toBeVisible();
        await expect(page.getByRole('link', { name: /board$/i }).first()).toBeVisible();
        const cancel = page.getByRole('button', { name: 'Cancel assembly' });
        if (a.done) await expect(cancel).toHaveCount(0);
        else await expect(cancel).toBeVisible();
        await expectNoHorizontalScroll(page);
        await testInfo.attach(`assembly-${a.name}-${size.name}-${theme}`, {
          body: await page.screenshot({ fullPage: true }),
          contentType: 'image/png',
        });
      });
    }
  }
}

// A part row's pull request link must not intersect a part title link or a wave heading at the widths where the
// two columns are narrowest. Not run in the box (no browser).
const narrow = [
  { name: '1024', width: 1024, height: 768 },
  { name: '768', width: 768, height: 1024 },
];

for (const size of narrow) {
  test(`part rows keep the pull request link clear of titles and wave headings at ${size.name}`, async ({ page }) => {
    await page.setViewportSize({ width: size.width, height: size.height });
    await page.goto('/o/acme/f/goal/items/17');
    await expect(page.getByRole('heading', { name: 'Parts', exact: true })).toBeVisible();

    const prLinks = page.getByRole('link', { name: /^PR #\d+/ });
    const count = await prLinks.count();
    expect(count).toBeGreaterThan(0);
    const others = page.locator('main a[title], main h3');
    const boxes = [];
    for (const handle of await others.all()) {
      const box = await handle.boundingBox();
      if (box) boxes.push(box);
    }
    for (let i = 0; i < count; i++) {
      const pr = await prLinks.nth(i).boundingBox();
      if (!pr) continue;
      for (const b of boxes) {
        const apart =
          pr.x + pr.width <= b.x || b.x + b.width <= pr.x || pr.y + pr.height <= b.y || b.y + b.height <= pr.y;
        expect(apart).toBe(true);
      }
    }
    await expectNoHorizontalScroll(page);
  });
}

// An assembly of 24 parts (software#500): its meters fit a phone, on its page and as a row of the Items list.
test('the 24-part assembly fits a phone, on its page and in the Items list', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/o/acme/f/software/items/500');
  await expect(page.getByRole('heading', { name: 'Progress' })).toBeVisible();
  await page.waitForLoadState('networkidle');
  await expectNoHorizontalScroll(page);

  await page.goto('/o/acme/f/software/items');
  const meter = page.getByTestId('assembly-meter').first();
  await expect(meter).toBeVisible();
  await page.waitForLoadState('networkidle');
  await expectNoHorizontalScroll(page);
  const fits = await meter.evaluate((el) => el.getBoundingClientRect().right <= document.documentElement.clientWidth);
  expect(fits).toBe(true);
});
