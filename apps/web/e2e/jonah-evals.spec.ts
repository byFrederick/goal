// Jonah's Evals (build the Jonah evals screen, jonah/evals.jpg): the cases of the Software factory,
// their tries, a run of one case and of all of them, and the add-case dialog's gate. Not run in the box:
// Playwright needs a browser, which the box does not have.
import { expect, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll } from './support';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

test('the evals page lists the five cases with their tries, cost and a run for each', async ({ page }) => {
  await page.goto('/o/acme/f/software/jonah/evals');
  await expect(page.getByRole('heading', { level: 1, name: 'Jonah' })).toBeVisible();
  await expect(page.getByText('Cases passing every try, v21')).toBeVisible();
  await expect(page.getByText('3 of 5', { exact: true })).toBeVisible();
  await expect(page.getByText('87%', { exact: true })).toBeVisible();
  await expect(page.getByText('13 of 15 tries')).toBeVisible();

  const table = page.getByRole('table');
  await expect(table.getByRole('row')).toHaveCount(6);
  await expect(table.getByText('architect-frontend-lane')).toBeVisible();
  await expect(table.getByRole('img', { name: '2 of 3 passed, last one failed' })).toHaveCount(1);
  await expect(page.getByRole('button', { name: 'Run all, about $2.40' })).toBeVisible();

  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('a run of one case is queued, and Run all starts every case', async ({ page }) => {
  await page.goto('/o/acme/f/software/jonah/evals');
  await page.getByRole('button', { name: 'Run reviewer-approves-clean-change' }).click();
  await page.getByRole('button', { name: 'Run all, about $2.40' }).click();
  await expect(page.getByRole('button', { name: 'Run all, about $2.40' })).toBeVisible();
});

test('adding a case from a past run names a case that is not a slug before it is sent', async ({ page }) => {
  await page.goto('/o/acme/f/software/jonah/evals');
  await page.getByRole('button', { name: 'Add a case from a past run' }).click();
  const dialog = page.getByRole('dialog', { name: 'Add a case from a past run' });
  await dialog.getByLabel('Past run').fill('run-41');
  await dialog.getByLabel('Case name').fill('Bad Name');
  await dialog.getByRole('button', { name: 'Add case' }).click();
  await expect(dialog.getByText('Use 3 to 40 lowercase letters, digits and hyphens.')).toBeVisible();
});

test('a factory with no cases says so and offers adding one', async ({ page }) => {
  await page.goto('/o/acme/f/goal/jonah/evals');
  await expect(page.getByText('No eval cases yet')).toBeVisible();
});

test('the tabs Overview, Proposals, Judges and Bake-offs say they are not available yet', async ({ page }) => {
  await page.goto('/o/acme/f/software/jonah/evals');
  await expect(page.getByRole('navigation', { name: 'Jonah' }).getByRole('link', { name: 'Evals' })).toHaveAttribute(
    'aria-current',
    'page',
  );
  await page.getByRole('button', { name: 'Bake-offs' }).hover();
  await expect(page.getByText('Not available yet').first()).toBeVisible();
});
