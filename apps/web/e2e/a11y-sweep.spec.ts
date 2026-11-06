// axe on the Floor, Definition's line, Jonah, Home and Spend at a phone and a desktop width in both themes, and the 44 px
// targets of Definition's line at 390 px. Not run in the box: Playwright needs a browser, which the box does not have.
import { expect, test } from '@playwright/test';
import { expectAccessible } from './support';
import { expectTarget44 } from './support-boxes';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

const routes = [
  '/o/acme/f/goal',
  '/o/acme/f/goal/definition/line',
  '/o/acme/f/software/jonah',
  '/o/acme',
  '/o/acme/spend',
];

for (const scheme of ['light', 'dark'] as const) {
  for (const width of [390, 1280]) {
    for (const path of routes) {
      test(`${path} has no axe violations at ${width} px in ${scheme}`, async ({ page }) => {
        await page.setViewportSize({ width, height: 900 });
        await page.emulateMedia({ colorScheme: scheme });
        await page.goto(path);
        await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
        await expectAccessible(page);
      });
    }
  }
}

test('Definition line targets are 44 px at 390 px', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 900 });
  await page.goto('/o/acme/f/goal/definition/line');
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
  await expectTarget44(page.getByRole('link', { name: /^Edit in YAML/ }).first(), 'Edit in YAML');
  await expectTarget44(page.getByRole('button', { name: /^Rules of/ }).first(), 'Rules of station');
  await expectTarget44(page.getByRole('button', { name: /^Drag / }).first(), 'station drag handle');
});
