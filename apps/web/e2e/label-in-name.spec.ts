// WCAG 2.5.3: a control's accessible name contains its visible text (axe's label-content-name-mismatch, which axe tags
// experimental, so expectAccessible never runs it). Checked in the shell and on Jonah's Tests at a desktop and a phone width.
// Not run in the box: Playwright needs a browser, which the box does not have.
import { expect, test } from '@playwright/test';
import { expectLabelInName, orgNav } from './support';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

const routes = [
  '/o/acme/f/goal',
  '/o/acme/knowledge',
  '/o/acme/f/software/jonah/tests',
  '/o/acme/f/goal/definition/line',
  '/o/acme/f/software/jonah',
  '/o/acme',
  '/o/acme/spend',
];

for (const width of [1280, 390]) {
  for (const path of routes) {
    test(`names contain their visible text on ${path} at ${width} px`, async ({ page }) => {
      await page.setViewportSize({ width, height: 900 });
      await page.goto(path);
      await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
      // The sidebar from 768 px, the phone bar below it.
      const chrome = width >= 768 ? orgNav(page) : page.getByRole('navigation', { name: 'Tabs' });
      await expect(chrome.filter({ visible: true })).toBeVisible();
      await expectLabelInName(page);
    });
  }
}
