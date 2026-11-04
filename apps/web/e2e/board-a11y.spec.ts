// The Board's accessibility: axe's WCAG rules (colour contrast included), names that contain their visible text
// (WCAG 2.5.3) and exactly one h1 (axe's page-has-heading-one is a best-practice rule expectAccessible does not run).
// Not run in the box: Playwright needs a browser, which the box does not have.
import { expect, test } from '@playwright/test';
import { expectAccessible, expectLabelInName } from './support';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

for (const scheme of ['light', 'dark'] as const) {
  for (const width of [390, 1280] as const) {
    test.describe(`Board, ${scheme} at ${width} px`, () => {
      test.use({ viewport: { width, height: 900 }, colorScheme: scheme });

      test('has one h1, passes axe and its names contain their visible text', async ({ page }) => {
        await page.goto('/o/acme/f/software/board');
        await expect(page.getByRole('heading', { level: 1, name: 'Board' })).toHaveCount(1);
        await expect(page.getByRole('heading', { level: 1 })).toHaveCount(1);
        await expectAccessible(page);
        await expectLabelInName(page);
      });
    });
  }
}
