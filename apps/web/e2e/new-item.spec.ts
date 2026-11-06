// New item from the sidebar (MASTER.md "The shell"): C, or the button, opens the composer with a
// factory picker. The picker starts on the open factory, is empty elsewhere, and lists only the
// factories the person may add items to.
import { expect, type Page, test } from '@playwright/test';
import { pinClock } from './support';

// One instant for the mock and the page: noon UTC (support.ts).
test.beforeEach(async ({ page, request }) => {
  await pinClock(page, request);
});

// The shortcut listens once the shell has mounted, which is after the page's load event. Press C until
// the composer opens; once it is open, a further C does nothing (a dialog is open).
async function openWithC(page: Page) {
  await expect(async () => {
    await page.keyboard.press('c');
    await expect(page.getByRole('dialog', { name: 'New item' })).toBeVisible({ timeout: 500 });
  }).toPass({ timeout: 10_000 });
}

test('C opens the composer on the open factory and creates the item there', async ({ page }) => {
  await page.goto('/o/acme/f/goal');
  await openWithC(page);

  const dialog = page.getByRole('dialog', { name: 'New item' });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('combobox', { name: 'Factory' })).toHaveText('goal');
  await dialog.getByRole('textbox', { name: 'Title' }).fill('Check this week’s spend');
  await dialog.getByRole('button', { name: 'Create item' }).click();

  await expect(page).toHaveURL(/\/o\/acme\/f\/goal\/items\//);
});

test('elsewhere the picker starts empty and asks for a factory', async ({ page }) => {
  await page.goto('/o/acme');
  await openWithC(page);

  const dialog = page.getByRole('dialog', { name: 'New item' });
  const picker = dialog.getByRole('combobox', { name: 'Factory' });
  await expect(picker).toHaveText('Choose a factory');
  await dialog.getByRole('textbox', { name: 'Title' }).fill('Check the budget');
  await dialog.getByRole('button', { name: 'Create item' }).click();

  await expect(dialog.getByText('Choose a factory for the item.')).toBeVisible();
});

test('the picker leaves out a factory where the person may not add items', async ({ page }) => {
  await page.goto('/o/acme');
  await openWithC(page);

  const picker = page.getByRole('dialog', { name: 'New item' }).getByRole('combobox', { name: 'Factory' });
  await picker.click();
  const list = page.getByRole('listbox');
  await expect(list.getByRole('option', { name: 'goal' })).toHaveCount(1);
  await expect(list.getByRole('option', { name: 'sre' })).toHaveCount(1);
  await expect(list.getByRole('option', { name: 'support' })).toHaveCount(0);
});
