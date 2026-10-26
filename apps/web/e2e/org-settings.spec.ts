// Organization settings (C §13 Playwright 5): rename, short mark, address, slot limits, the
// unsaved-changes prompt and Delete, at every width against the mock.
import { expect, type Page, test } from '@playwright/test';
import {
  expectAccessible,
  expectNoHorizontalScroll,
  expectToast,
  isPhone,
  orgMarkOf,
  orgNav,
  recordToasts,
} from './support';

test.beforeEach(async ({ page, request }) => {
  await request.post('/__mock/reset');
  await recordToasts(page);
});

const heading = (page: Page) => page.getByRole('heading', { level: 1, name: 'Organization settings' });
const save = (page: Page) => page.getByRole('button', { name: 'Save changes' });
const slotsFor = (page: Page) => page.getByRole('group', { name: 'Most slots each factory may use' });

test('organization settings is accessible and fits the screen', async ({ page }) => {
  await page.goto('/o/acme/settings');
  await expect(heading(page)).toBeVisible();
  await expect(page).toHaveTitle('Settings · Acme · Goal');
  await expect(page.getByText('Settings for Acme and all 3 of its factories.')).toBeVisible();
  await expect(page.getByLabel('Name')).toHaveValue('Acme');
  await expect(page.getByLabel('Short mark')).toHaveValue('Ac');
  await expect(page.getByLabel('Address')).toHaveValue('acme');
  await expect(page.getByLabel('Slots shared by every factory')).toHaveValue('6');
  await expect(slotsFor(page).getByLabel('goal')).toHaveValue('3');
  await expect(slotsFor(page).getByLabel('support')).toHaveValue('');
  // Model access names the organization's model connections; Secrets points at Connections. Transfer
  // ownership needs sign-in, which is off here.
  await expect(
    page.getByText(/Acme has one: claude, Anthropic API key, used by goal, sre and support\./),
  ).toBeVisible();
  await expect(page.getByRole('link', { name: 'Open Connections' })).toHaveAttribute('href', '/o/acme/connections');
  await expect(page.getByRole('radio')).toHaveCount(0);
  await expect(page.getByText(/Transfer/)).toHaveCount(0);
  await expect(save(page)).toBeDisabled();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('the organization sidebar opens Settings', async ({ page }) => {
  test.skip(isPhone(page), 'sidebars are the 768 px and wider navigation');
  await page.goto('/o/acme');
  const sidebar = page.getByRole('navigation', { name: 'Acme organization' });
  await sidebar.getByRole('link', { name: 'Settings' }).click();
  await expect(page).toHaveURL(/\/o\/acme\/settings$/);
  await expect(sidebar.getByRole('link', { name: 'Settings' })).toHaveAttribute('aria-current', 'page');
  await expect(heading(page)).toBeFocused();
});

test('a new name and short mark show in the rail once saved', async ({ page }) => {
  await page.goto('/o/acme/settings');
  await page.getByLabel('Name').fill('Acme Robotics');
  await page.getByLabel('Short mark').fill('AR');
  await save(page).click();

  await expectToast(page, 'Saved');
  await expect(save(page)).toBeDisabled();
  await expect(orgMarkOf(page, 'AR, Acme Robotics organization, switch organization')).toBeVisible();
  await expect(page.getByText('Settings for Acme Robotics and all 3 of its factories.')).toBeVisible();
  await expect(page).toHaveTitle('Settings · Acme Robotics · Goal');
});

test('a new address moves the page to it, and the old one stops working', async ({ page }) => {
  await page.goto('/o/acme/settings');
  const address = page.getByLabel('Address');
  await address.fill('acme-co');
  await expect(address).toHaveAccessibleDescription(/\/o\/acme-co\s*Links with the old address stop working\./);
  await save(page).click();

  await expect(page).toHaveURL(/\/o\/acme-co\/settings$/);
  await expect(address).toHaveValue('acme-co');
  await expect(page.getByText('Links with the old address stop working.')).toHaveCount(0);
  expect(await page.evaluate(() => localStorage.getItem('goal.org'))).toBe('acme-co');

  await page.goto('/o/acme');
  await expect(page.getByRole('heading', { level: 1, name: 'No such organization' })).toBeVisible();
  await page.goto('/');
  await expect(page).toHaveURL(/\/o\/acme-co$/);
});

test('an address another organization has is the field’s error', async ({ page }) => {
  await page.goto('/o/acme/settings');
  const address = page.getByLabel('Address');
  await address.fill('example-labs');
  await save(page).click();

  await expect(address).toHaveAttribute('aria-invalid', 'true');
  await expect(address).toHaveAccessibleDescription(/^Taken/);
  await expect(page).toHaveURL(/\/o\/acme\/settings$/);
  await address.fill('acme-labs');
  await expect(address).not.toHaveAttribute('aria-invalid', 'true');
});

test('values goald would refuse are shown at their field before saving', async ({ page }) => {
  await page.goto('/o/acme/settings');
  await page.getByLabel('Short mark').fill('');
  await slotsFor(page).getByLabel('sre').fill('0');
  await save(page).click();

  const mark = page.getByLabel('Short mark');
  await expect(mark).toBeFocused();
  await expect(mark).toHaveAccessibleDescription(/^One or two letters or digits\./);
  await expect(slotsFor(page).getByLabel('sre')).toHaveAccessibleDescription(
    'A whole number from 1 to 1000, or blank for no limit.',
  );
  await expectAccessible(page);
});

test('slot limits: per factory and across the organization', async ({ page, request }) => {
  await page.goto('/o/acme/settings');
  await expect(page.getByText('A factory without a limit may use all 6 slots.')).toBeVisible();

  await slotsFor(page).getByLabel('support').fill('3');
  await expect(
    page.getByText('Limits add up to 8 of 6 slots, so factories wait for each other at peak. That’s allowed.'),
  ).toBeVisible();
  await save(page).click();
  await expectToast(page, 'Saved');

  await page.getByLabel('Slots shared by every factory').fill('');
  await save(page).click();
  await expect(page.getByText('No limit across the organization: each factory stops at its own limit.')).toBeVisible();
  await expect(save(page)).toBeDisabled();

  const settings = await (await request.get('/v1/orgs/acme/settings')).json();
  expect(settings.slots.max).toBeNull();
  expect(settings.slots.factories).toContainEqual(expect.objectContaining({ name: 'support', max: 3 }));
  await page.reload();
  await expect(slotsFor(page).getByLabel('support')).toHaveValue('3');
});

test('leaving with unsaved changes asks first', async ({ page }) => {
  await page.goto('/o/acme/settings');
  await page.getByLabel('Name').fill('Acme Robotics');
  const goal = orgNav(page).getByRole('link', { name: /^G, goal factory/ });

  await goal.click();
  const prompt = page.getByRole('dialog', { name: 'Leave without saving?' });
  await expect(prompt.getByRole('button', { name: 'Stay' })).toBeFocused();
  await expectAccessible(page);
  await prompt.getByRole('button', { name: 'Stay' }).click();
  await expect(prompt).toBeHidden();
  await expect(page).toHaveURL(/\/o\/acme\/settings$/);
  await expect(page.getByLabel('Name')).toHaveValue('Acme Robotics');

  await goal.click();
  await prompt.getByRole('button', { name: 'Leave' }).click();
  await expect(page).toHaveURL(/\/o\/acme\/f\/goal$/);
});

test('delete is enabled only by the typed name, then opens the next organization', async ({ page, request }) => {
  await page.goto('/o/acme/settings');
  await expect(page.getByText('Deletes 3 factories and their history. You type the name to confirm.')).toBeVisible();
  await page.getByRole('button', { name: 'Delete organization…' }).click();

  const dialog = page.getByRole('dialog', { name: 'Delete Acme?' });
  const typed = dialog.getByLabel('Type Acme to confirm');
  const confirm = dialog.getByRole('button', { name: 'Delete organization' });
  await expect(typed).toBeFocused();
  await expect(confirm).toBeDisabled();
  await typed.fill('acme');
  await expect(confirm).toBeDisabled();
  await expectAccessible(page);
  await typed.fill('Acme');
  await confirm.click();

  await expect(page).toHaveURL(/\/o\/example-labs$/);
  await expectToast(page, 'Deleted Acme');
  expect((await request.get('/v1/orgs/acme')).status()).toBe(404);

  // The last organization: `/` says there is none.
  await page.goto('/o/example-labs/settings');
  await expect(page.getByText('Deletes 1 factory and its history. You type the name to confirm.')).toBeVisible();
  await page.getByRole('button', { name: 'Delete organization…' }).click();
  await page.getByLabel('Type Example Labs to confirm').fill('Example Labs');
  await page.getByRole('button', { name: 'Delete organization', exact: true }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'No organization yet' })).toBeVisible();
});
