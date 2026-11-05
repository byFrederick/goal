// Intake (intake, ADR 0026) against the mock: the software factory's triggers and sources, the switch, and
// the New trigger picker. Not run in the box (no browser): the Vitest suites cover the same behavior.
import { expect, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll, isPhone } from './support';
import { expectTarget44 } from './support-boxes';

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
  // Every trigger of the mock's definition is on; the rebuilt Intake (software#549) has no suggested, off ones.
  await expect(triggers.getByRole('switch', { name: 'deploy-failed' })).toHaveAttribute('aria-checked', 'true');
  await expect(page.getByRole('region', { name: 'Webhooks open to Software' })).toBeVisible();

  // A phone's bar keeps New trigger only; the webhooks card carries the link there.
  await expect(page.locator('a:visible[href$="/intake/webhooks"]').first()).toHaveAttribute('href', `${intake}/webhooks`);
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('New trigger opens the picker over the page and Continue goes to the trigger page', async ({ page }) => {
  await page.goto(intake);
  await page.getByRole('link', { name: 'New trigger' }).first().click();

  const dialog = page.getByRole('dialog', { name: 'New trigger' });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Slack' })).toHaveAttribute('aria-disabled', 'true');
  // A phone shows the sources first and one source's details after it is chosen.
  if (isPhone(page)) await dialog.getByRole('button', { name: 'GitHub' }).click();
  await expect(dialog.getByRole('radio', { name: 'Issue opened' })).toHaveAttribute('aria-disabled', 'true');

  await dialog.getByRole('radio', { name: 'Issue labeled' }).check();
  await dialog.getByRole('button', { name: 'Continue' }).click();
  // Continue leaves for the trigger page at …/intake/<free name>?on=<event key> (the picker's own contract).
  await expect(page).toHaveURL(new RegExp(`${intake}/[^/?]+\\?on=github\\.issues\\.labeled$`));
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
        // The open dialog is modal, so the top bar behind it is out of the accessibility tree.
        const nav = page.getByRole('navigation', { name: 'Breadcrumb', includeHidden: true });
        await expect(nav.getByText('Intake')).toBeAttached();
        await expect(nav.getByText('New trigger')).toBeAttached();
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

for (const scheme of ['light', 'dark'] as const) {
  for (const width of [390, 1440]) {
    test(`Intake and New trigger have no axe violations at ${width} px in ${scheme}`, async ({ page }) => {
      await page.setViewportSize({ width, height: 900 });
      await page.emulateMedia({ colorScheme: scheme });
      await page.goto(intake);
      await expect(page.getByRole('heading', { level: 1, name: 'Intake' })).toBeVisible();
      await expectAccessible(page);
      await page.goto(`${intake}/new`);
      await expect(page.getByRole('dialog', { name: 'New trigger' })).toBeVisible();
      await expectAccessible(page);
    });
  }
}

test('New trigger lists no tool permissions under Check with an agent', async ({ page }) => {
  await page.goto(`${intake}/new`);
  const dialog = page.getByRole('dialog', { name: 'New trigger' });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('button', { name: /^Bash/ })).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: /^(Edit|Write)/ })).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: /^github__/ })).toHaveCount(0);
});

test('Intake and New trigger targets are 44 px at 390 px', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 900 });
  await page.goto(intake);
  const coming = page.getByRole('region', { name: 'Coming up' });
  // The title link is stretched over the row, so the row is its hit area.
  await expectTarget44(coming.getByRole('listitem').first(), "a Coming up row's title link");
  await page.goto(`${intake}/new`);
  await expectTarget44(page.getByRole('searchbox', { name: 'Search sources' }), 'the source search');
});
