// The trigger page (docs/design/screens/intake/trigger.jpg) against the mock: the Software factory's
// flaky-test trigger, its blocks, its recent events, the disabled controls, a save that publishes a new
// version, the try with a past event and the model check, and the layout at 375, 768 and 1440 px in light and dark. Not run in the box (no browser):
// the pull request says so.
import { expect, type Page, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll, isPhone } from './support';

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
    // The figures are NumberRoll: the glyphs are aria-hidden and each figure is spoken once in an sr-only span.
    const week = page.getByRole('paragraph').filter({ hasText: 'items this week' });
    await expect(week.locator('.sr-only')).toHaveText(['3', '2', '$7.80']);
    await expect(week).toContainText('items this week');
    const when = page.getByRole('region', { name: 'When' });
    await expect(when.getByRole('combobox', { name: 'Event' })).toHaveValue('github.check_run');
    await expect(when.getByRole('group', { name: 'Action' })).toBeVisible();
    await expect(page.getByRole('region', { name: 'Recent events' })).toContainText('Created #150');
    const definition = page.getByRole('region', { name: 'In the definition' });
    await expect(definition.locator('pre')).toContainText('name: flaky-test-on-main');
    await expect(definition.getByRole('link', { name: 'Open file' })).toBeVisible();
    // The sentence is the About saving tip beside the In the definition heading.
    await definition.getByRole('button', { name: 'About saving' }).hover();
    await expect(page.getByText('Saving publishes a new version of Software’s definition.')).toBeVisible();
  });

  test('disables only the controls that are not built yet', async ({ page }) => {
    await page.goto(url);
    await expect(page.getByRole('button', { name: /Add another event/ })).toHaveAttribute('aria-disabled', 'true');
  });

  test('offers Try with a delivery for a GitHub trigger that has events', async ({ page }) => {
    await page.goto(url);
    if (isPhone(page)) {
      await page.getByRole('button', { name: 'More actions' }).click();
      await expect(page.getByRole('menuitem', { name: 'Try with a delivery' })).toBeEnabled();
    } else {
      await expect(page.getByRole('button', { name: 'Try with a delivery' })).toBeEnabled();
    }
  });

  test('runs a try of a schedule trigger against one of its past events, and creates nothing', async ({ page }) => {
    await page.goto('/o/acme/f/software/intake/dependency-audit');
    // On a phone the bar keeps only Save changes; the other actions sit behind More actions.
    if (isPhone(page)) {
      await page.getByRole('button', { name: 'More actions' }).click();
      await page.getByRole('menuitem', { name: 'Try with a past event' }).click();
    } else {
      await page.getByRole('button', { name: 'Try with a past event' }).click();
    }
    const dialog = page.getByRole('dialog', { name: 'Try with a past event' });
    await dialog.getByRole('button', { name: 'Run the try' }).click();
    await expect(dialog.getByText('The trigger would create an item.')).toBeVisible();
    await expect(dialog.getByText('Nothing was created. The try changed no item, event or limit.')).toBeVisible();
  });

  // ADR 0040 and software#645 removed the model check ("modelAgrees"): the page never asks about it.
  test('never offers the removed model check, and saves another edit', async ({ page }) => {
    await page.goto('/o/acme/f/software/intake/dependency-audit');
    await expect(page.getByRole('heading', { level: 1, name: 'Dependency audit' })).toBeVisible();
    await expect(page.getByRole('checkbox', { name: /model agrees/i })).toHaveCount(0);
    await expect(page.getByRole('textbox', { name: 'Question for the model' })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Save changes' })).toBeDisabled();
    await page.getByRole('checkbox', { name: /Pause while/ }).check();
    await page.getByRole('button', { name: 'Save changes' }).click();
    await expect(page.getByText("Saved. Software's definition is version 3 now.")).toBeVisible();
  });

  test('saves a change as a new version of the definition', async ({ page }) => {
    await page.goto(url);
    // The daily limit is an option now (off until its checkbox is on), so its count is disabled before.
    await page.getByRole('checkbox', { name: /items a day/ }).check();
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
        // The pull request's screenshots: 1440 and 375 px, light and dark (the Playwright output directory).
        if (width === 1440 || width === 375) {
          await page.screenshot({ path: `test-results/intake-trigger-${width}-${scheme}.png`, fullPage: true });
        }
      }
    });
  }
});
