// Intake (ADR 0026 and 0040) against the mock: the software factory's triggers and sources, the switch, and
// the New trigger picker. Not run in the box (no browser): the Vitest suites cover the same behavior.
import { expect, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll, isPhone } from './support';

const intake = '/o/acme/f/software/intake';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

test('the Intake page lists what is coming up, where items came from, the triggers and the webhooks', async ({
  page,
}) => {
  await page.goto(intake);
  await expect(page).toHaveTitle('Intake · software · Acme · Goal');
  await expect(page.getByRole('heading', { level: 1, name: 'Intake' })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Coming up' })).toBeVisible();
  await expect(page.getByRole('heading', { level: 2, name: 'Where items came from' })).toBeVisible();
  await expect(page.getByRole('heading', { level: 2, name: 'Follow-ups to triage' })).toBeVisible();

  const triggers = page.getByRole('region', { name: 'Triggers' });
  await expect(triggers.getByRole('link', { name: 'flaky-test-on-main' })).toBeVisible();
  await expect(triggers.getByText('2 events refused, bad signature')).toBeVisible();
  await expect(triggers.getByRole('switch', { name: 'deploy-failed' })).toHaveAttribute('aria-checked', 'false');
  await expect(page.getByRole('region', { name: 'Webhooks open to Software' })).toBeVisible();

  // The Webhooks button is in the top bar from 768 px; the card's Manage link reaches the same page on a phone.
  await expect(
    page.getByRole('region', { name: 'Webhooks open to Software' }).getByRole('link', { name: 'Manage' }),
  ).toHaveAttribute('href', `${intake}/webhooks`);
  if (!isPhone(page)) {
    await expect(page.getByRole('link', { name: 'Webhooks' })).toHaveAttribute('href', `${intake}/webhooks`);
  }
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('New trigger opens the picker over the page and Continue goes to the trigger page', async ({ page }) => {
  await page.goto(intake);
  await page.getByRole('link', { name: 'New trigger' }).first().click();

  const dialog = page.getByRole('dialog', { name: 'New trigger' });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Slack' })).toHaveAttribute('aria-disabled', 'true');
  // On a phone the picker is one pane: the events show once GitHub is chosen.
  if (isPhone(page)) await dialog.getByRole('button', { name: 'GitHub' }).click();
  await expect(dialog.getByRole('radio', { name: 'Issue opened' })).toHaveAttribute('aria-disabled', 'true');

  await dialog.getByRole('radio', { name: 'Issue labeled' }).check();
  await dialog.getByRole('button', { name: 'Continue' }).click();
  // The trigger page opens under the first free name for the event (ADR 0040, trigger-save).
  await expect(page).toHaveURL(`${intake}/new-issues-labeled?on=github.issues.labeled`);
});

test('the picker is one pane with Back on a phone', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 800 });
  await page.goto(`${intake}/new`);
  const dialog = page.getByRole('dialog', { name: 'New trigger' });
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: 'Schedule' }).click();
  await expect(dialog.getByRole('button', { name: 'Back' })).toBeVisible();
  await dialog.getByRole('button', { name: 'Back' }).click();
  await expect(dialog.getByRole('button', { name: 'Schedule' })).toBeVisible();
  await expectNoHorizontalScroll(page);
});

// The picker at the desktop and phone widths in both themes: plain icons, one suggestion per row, when the factory has them, the breadcrumb.
for (const theme of ['light', 'dark'] as const) {
  for (const size of [
    { width: 1440, height: 900 },
    { width: 390, height: 844 },
  ]) {
    test(`the New trigger picker at ${size.width}px, ${theme}`, async ({ page }) => {
      await page.addInitScript((value) => window.localStorage.setItem('goal-theme', value), theme);
      await page.setViewportSize(size);
      await page.goto(`${intake}/new`);
      const dialog = page.getByRole('dialog', { name: 'New trigger' });
      await expect(dialog).toBeVisible();
      await expect(dialog.getByRole('button', { name: 'Close' })).not.toBeFocused();

      const github = dialog.getByRole('button', { name: 'GitHub' });
      await expect(github.locator('[class*="bg-"]')).toHaveCount(0);

      const rows = dialog.getByRole('listitem').filter({ has: page.getByRole('button', { name: 'Turn on' }) });
      if ((await rows.count()) >= 2) {
        const first = await rows.nth(0).boundingBox();
        const second = await rows.nth(1).boundingBox();
        expect(second?.y ?? 0).toBeGreaterThan((first?.y ?? 0) + (first?.height ?? 0) - 1);
        expect(second?.x).toBe(first?.x);
      }

      if (size.width >= 1440) {
        // The open dialog hides the page from the accessibility tree, so the bar is found by its label.
        const nav = page.locator('nav[aria-label="Breadcrumb"]');
        await expect(nav.getByText('Intake')).toBeVisible();
        await expect(nav.getByText('New trigger')).toBeVisible();
      }
      await expectAccessible(page);
      await expectNoHorizontalScroll(page);
    });
  }
}

for (const colorScheme of ['light', 'dark'] as const) {
  for (const width of [1440, 768, 390]) {
    test(`no element is wider than the viewport at ${width} px in ${colorScheme}`, async ({ page }) => {
      await page.emulateMedia({ colorScheme });
      await page.setViewportSize({ width, height: 900 });
      await page.goto(intake);
      await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
      await expectNoHorizontalScroll(page);
    });
  }
}
