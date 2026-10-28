// The trigger page (docs/design/screens/intake/trigger.jpg) against the mock: the Software factory's
// flaky-test trigger, its blocks, its recent events, the disabled controls, a save that publishes a new
// version, the try with a past event and the model check, and the layout at 375, 768 and 1440 px in light and dark. Not run in the box (no browser):
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

  test('disables only the controls that are not built yet', async ({ page }) => {
    await page.goto(url);
    await expect(page.getByRole('button', { name: /Add another event/ })).toHaveAttribute('aria-disabled', 'true');
  });

  test('says why Try is unavailable for a GitHub trigger', async ({ page }) => {
    await page.goto(url);
    await expect(page.getByRole('button', { name: 'Try with a past event' })).toBeDisabled();
    await expect(
      page.getByText("Only a schedule's events are stored, so this trigger's events cannot be tried."),
    ).toBeVisible();
  });

  test('runs a try of a schedule trigger against one of its past events, and creates nothing', async ({ page }) => {
    await page.goto('/o/acme/f/software/intake/dependency-audit');
    await page.getByRole('button', { name: 'Try with a past event' }).click();
    const dialog = page.getByRole('dialog', { name: 'Try with a past event' });
    await dialog.getByRole('button', { name: 'Run the try' }).click();
    await expect(dialog.getByText('The trigger would create an item.')).toBeVisible();
    await expect(dialog.getByText('Nothing was created. The try changed no item, event or limit.')).toBeVisible();
  });

  test('shows the question field when the model check is checked, and keeps Save off while it is blank', async ({
    page,
  }) => {
    await page.goto('/o/acme/f/software/intake/dependency-audit');
    const check = page.getByRole('checkbox', { name: /A model agrees it is worth an item/ });
    await check.check();
    const question = page.getByRole('textbox', { name: 'Question for the model' });
    await expect(question).toBeVisible();
    await question.fill('');
    await expect(page.getByRole('button', { name: 'Save changes' })).toBeDisabled();
    await question.fill('Does this audit name a fix for a direct dependency?');
    await page.getByRole('button', { name: 'Save changes' }).click();
    await expect(page.getByText("Saved. Software's definition is version 3 now.")).toBeVisible();
    await expect(page.getByRole('region', { name: 'In the definition' }).locator('pre')).toContainText('modelAgrees');
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
