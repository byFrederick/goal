// Intake (intake, ADR 0026) against the mock: the software factory's triggers and sources, the switch, and
// the New trigger picker. Not run in the box (no browser): the Vitest suites cover the same behavior.
import { expect, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll } from './support';

const intake = '/o/acme/f/software/intake';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

test('the Intake page lists where work came from, the triggers and the suggested triggers', async ({ page }) => {
  await page.goto(intake);
  await expect(page).toHaveTitle('Intake · Software · Acme · Goal');
  await expect(page.getByRole('heading', { level: 1, name: 'Intake' })).toBeVisible();
  await expect(page.getByRole('heading', { level: 2, name: 'Where the last 48 items came from' })).toBeVisible();

  const triggers = page.getByRole('region', { name: 'Triggers' });
  await expect(triggers.getByRole('link', { name: 'flaky-test-on-main' })).toBeVisible();
  await expect(triggers.getByText('2 events refused, bad signature')).toBeVisible();
  await expect(triggers.getByRole('switch', { name: 'deploy-failed' })).toHaveAttribute('aria-checked', 'false');
  await expect(page.getByRole('heading', { level: 2, name: 'Suggested triggers' })).toBeVisible();

  await expect(page.getByRole('link', { name: 'Webhooks' })).toHaveAttribute('href', `${intake}/webhooks`);
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('New trigger opens the picker over the page and Continue goes to the trigger page', async ({ page }) => {
  await page.goto(intake);
  await page.getByRole('link', { name: 'New trigger' }).first().click();

  const dialog = page.getByRole('dialog', { name: 'New trigger' });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Slack' })).toHaveAttribute('aria-disabled', 'true');
  await expect(dialog.getByRole('radio', { name: 'Issue opened' })).toHaveAttribute('aria-disabled', 'true');

  await dialog.getByRole('radio', { name: 'Issue labeled' }).check();
  await dialog.getByRole('button', { name: 'Continue' }).click();
  await expect(page).toHaveURL(`${intake}/new/github?on=github.issues.labeled`);
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
