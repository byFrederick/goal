// New item from the sidebar (MASTER.md "The shell"): C, or the button, opens the composer with a
// factory picker. The picker starts on the open factory, is empty elsewhere, and lists only the
// factories the person may add items to.
import { expect, test } from '@playwright/test';

// One instant for the test, the mock and the page (the same as floor.spec.ts).
const now = new Date(`${new Date().toISOString().slice(0, 10)}T12:00:00Z`);

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset', { data: { now: now.toISOString() } });
});

test('C opens the composer on the open factory and creates the item there', async ({ page }) => {
  await page.goto('/o/acme/f/goal');
  await page.keyboard.press('c');

  const dialog = page.getByRole('dialog', { name: 'New item' });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('combobox', { name: 'Factory' })).toHaveValue('goal');
  await dialog.getByRole('textbox', { name: 'Title' }).fill('Check this week’s spend');
  await dialog.getByRole('button', { name: 'Create item' }).click();

  await expect(page).toHaveURL(/\/o\/acme\/f\/goal\/items\//);
});

test('elsewhere the picker starts empty and asks for a factory', async ({ page }) => {
  await page.goto('/o/acme');
  await page.keyboard.press('c');

  const dialog = page.getByRole('dialog', { name: 'New item' });
  const picker = dialog.getByRole('combobox', { name: 'Factory' });
  await expect(picker).toHaveValue('');
  await dialog.getByRole('textbox', { name: 'Title' }).fill('Check the budget');
  await dialog.getByRole('button', { name: 'Create item' }).click();

  await expect(dialog.getByText('Choose a factory for the item.')).toBeVisible();
});

test('the picker leaves out a factory where the person may not add items', async ({ page }) => {
  await page.goto('/o/acme');
  await page.keyboard.press('c');

  const picker = page.getByRole('dialog', { name: 'New item' }).getByRole('combobox', { name: 'Factory' });
  await expect(picker.getByRole('option', { name: 'goal' })).toHaveCount(1);
  await expect(picker.getByRole('option', { name: 'sre' })).toHaveCount(1);
  await expect(picker.getByRole('option', { name: 'support' })).toHaveCount(0);
});
