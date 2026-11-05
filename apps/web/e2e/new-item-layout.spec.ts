// New item's Repositories field (canvas mobile/new-item) against the mock, with two repositories picked and
// the Add popover open: at 768 and 390 px, in light and dark, nothing goes past the viewport edge, no text
// is clipped, and a phone's tap targets are 44 px. These specs are not run in the box (no browser); CI runs them.
import { expect, type Page, test } from '@playwright/test';
import { expectNoClippedElements, expectNoHorizontalScroll } from './support';
import { expectTarget44 } from './support-boxes';

const sizes = [
  { width: 768, height: 1024 },
  { width: 390, height: 844 },
];

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

/** Opens New item on the goal factory and picks a second repository, then opens Add again. */
async function openWithTwoRepositories(page: Page) {
  await page.goto('/o/acme/f/goal');
  await page.keyboard.press('c');
  const dialog = page.getByRole('dialog', { name: 'New item' });
  await expect(dialog).toBeVisible();
  const field = dialog.getByRole('group', { name: 'Repositories' });
  await field.getByRole('button', { name: 'Add' }).click();
  await page.getByRole('option').first().click();
  await expect(field.getByRole('button', { name: /^Remove / })).toHaveCount(2);
  await field.getByRole('button', { name: 'Add' }).click();
  return { dialog, field };
}

for (const size of sizes) {
  for (const scheme of ['light', 'dark'] as const) {
    test(`New item's repositories fit ${size.width} px in ${scheme}`, async ({ page }, testInfo) => {
      test.skip(testInfo.project.name !== 'desktop-1440', 'one project runs the whole matrix');
      await page.setViewportSize(size);
      await page.emulateMedia({ colorScheme: scheme });
      await openWithTwoRepositories(page);

      await expectNoHorizontalScroll(page);
      await expectNoClippedElements(page);
      await testInfo.attach(`new-item-${size.width}-${scheme}`, {
        body: await page.screenshot(),
        contentType: 'image/png',
      });
    });
  }
}

test('the chips’ Remove buttons and the ⓘ are 44 px on a phone', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop-1440', 'one project runs the whole matrix');
  await page.setViewportSize({ width: 390, height: 844 });
  const { field } = await openWithTwoRepositories(page);
  await page.keyboard.press('Escape');
  await expectTarget44(field.getByRole('button', { name: 'About Repositories' }), 'About Repositories');
  for (const remove of await field.getByRole('button', { name: /^Remove / }).all()) {
    await expectTarget44(remove, 'Remove');
  }
});
