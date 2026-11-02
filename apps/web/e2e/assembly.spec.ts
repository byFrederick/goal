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
