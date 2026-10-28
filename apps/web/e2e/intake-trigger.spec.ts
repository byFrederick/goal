// The trigger page (docs/design/screens/intake/trigger.jpg) against the mock: the Software factory's
// flaky-test trigger, its blocks, its recent events, the disabled controls, a save that publishes a new
// version, and the layout at 375, 768 and 1440 px in light and dark. Not run in the box (no browser):
// the pull request says so.
import { expect, type Page, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll } from './support';

const url = '/o/acme/f/software/intake/flaky-test-on-main';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

async function fits(page: Page) {
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
}

test.describe('the trigger page', () => {
  test('shows the trigger as the picture does, with its week, events and YAML', async ({ page }) => {
    await page.goto(url);
    await expect(page.getByRole('heading', { level: 1, name: 'Flaky test on main' })).toBeVisible();
    await expect(page.getByText('Created 3 items this week, 2 merged, $7.80 in all')).toBeVisible();
    const when = page.getByRole('region', { name: 'When' });
    await expect(when.getByRole('combobox', { name: 'Event' })).toHaveValue('github.check_run.failed');
    await expect(page.getByRole('region', { name: 'Recent events' })).toContainText('Created #150');
    const definition = page.getByRole('region', { name: 'In the definition' });
    await expect(definition.locator('pre')).toContainText('name: flaky-test-on-main');
    await expect(definition.getByRole('link', { name: 'Open file' })).toBeVisible();
    await expect(page.getByText("Saving publishes a new version of Software's definition.")).toBeVisible();
  });

  test('disables the controls that are not built yet', async ({ page }) => {
    await page.goto(url);
    for (const name of [/Try with a past event/, /A model agrees it is worth an item/, /Add another event/]) {
      await expect(page.getByRole('button', { name })).toHaveAttribute('aria-disabled', 'true');
    }
  });

  test('saves a change as a new version of the definition', async ({ page }) => {
    await page.goto(url);
    await page.getByRole('spinbutton', { name: 'Items a day' }).fill('5');
    await page.getByRole('button', { name: 'Save changes' }).click();
    await expect(page.getByText("Saved. Software's definition is version 3 now.")).toBeVisible();
    await expect(page.getByRole('button', { name: 'Save changes' })).toBeDisabled();
  });

  for (const width of [375, 768, 1440]) {
    test(`fits at ${width} px in light and dark`, async ({ page }) => {
      await page.setViewportSize({ width, height: 1000 });
      for (const scheme of ['light', 'dark'] as const) {
        await page.emulateMedia({ colorScheme: scheme });
        await page.goto(url);
        await expect(page.getByRole('heading', { level: 1, name: 'Flaky test on main' })).toBeVisible();
        await fits(page);
      }
    });
  }
});
