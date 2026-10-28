// Organization settings (C §13 Playwright 5, restyled to docs/design/screens/organization/org-settings):
// rename, address, slot limits, the controls not built yet, the unsaved-changes prompt, Transfer
// ownership and Delete, at every width against the mock.
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
  await expect(page.getByLabel('Name')).toHaveValue('Acme');
  await expect(page.getByLabel('Address')).toHaveValue('acme');
  await expect(page.getByLabel('Slots shared by every factory')).toHaveValue('6');
  await expect(slotsFor(page).getByLabel('goal')).toHaveValue('3');
  await expect(slotsFor(page).getByLabel('support')).toHaveValue('');
  // There is no short mark, model access or secrets on the page now.
  await expect(page.getByLabel('Short mark')).toHaveCount(0);
  await expect(page.getByText('Model access')).toHaveCount(0);
  // Email code is on and Google is off; both are shown, not changed, until the API has them.
  await expect(page.getByRole('switch', { name: 'Email code sign-in' })).toHaveAttribute('aria-checked', 'true');
  await expect(page.getByRole('switch', { name: 'Google sign-in' })).toHaveAttribute('aria-disabled', 'true');
  // Transfer ownership needs sign-in, which is off here.
  await expect(page.getByLabel('Transfer ownership')).toHaveCount(0);
  await expect(save(page)).toBeDisabled();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('controls the API does not have yet are disabled and send nothing', async ({ page }) => {
  await page.goto('/o/acme/settings');
  const google = page.getByRole('switch', { name: 'Google sign-in' });
  await google.click();
  await expect(google).toHaveAttribute('aria-checked', 'false');
  for (const name of ['Set up', 'Use your own bucket', 'Open audit log', 'Export']) {
    await expect(page.getByRole('button', { name, exact: true })).toHaveAttribute('aria-disabled', 'true');
  }
  await expect(save(page)).toBeDisabled();
});

test('the organization menu opens Organization settings', async ({ page }) => {
  test.skip(isPhone(page), 'the organization menu is the 768 px and wider navigation');
  await page.goto('/o/acme');
  await orgMarkOf(page).click();
  await page.getByRole('menuitem', { name: 'Organization settings' }).click();
  await expect(page).toHaveURL(/\/o\/acme\/settings$/);
  await expect(heading(page)).toBeFocused();
});

test('a new name shows in the sidebar and the top row once saved', async ({ page }) => {
  await page.goto('/o/acme/settings');
  await page.getByLabel('Name').fill('Acme Robotics');
  await save(page).click();

  await expectToast(page, 'Saved');
  await expect(save(page)).toBeDisabled();
  await expect(orgMarkOf(page, 'Acme Robotics')).toBeVisible();
  await expect(page).toHaveTitle('Settings · Acme Robotics · Goal');
});

test('a new address moves the page to it, and the old one stops working', async ({ page }) => {
  await page.goto('/o/acme/settings');
  const address = page.getByLabel('Address');
  await address.fill('acme-co');
  await expect(address).toHaveAccessibleDescription(/Changing it breaks old links/);
  await expect(page.getByText('Links with the old address stop working.')).toBeVisible();
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
  await address.fill('goal');
  await save(page).click();

  await expect(address).toHaveAttribute('aria-invalid', 'true');
  await expect(address).toHaveAccessibleDescription(/Taken/);
  await expect(page).toHaveURL(/\/o\/acme\/settings$/);
  await address.fill('acme-labs');
  await expect(address).not.toHaveAttribute('aria-invalid', 'true');
});

test('values goald would refuse are shown at their field before saving', async ({ page }) => {
  await page.goto('/o/acme/settings');
  await page.getByLabel('Name').fill('');
  await slotsFor(page).getByLabel('sre').fill('0');
  await save(page).click();

  const name = page.getByLabel('Name');
  await expect(name).toBeFocused();
  await expect(name).toHaveAccessibleDescription(/Give the organization a name\./);
  await expect(slotsFor(page).getByLabel('sre')).toHaveAccessibleDescription(
    /A whole number from 1 to 1000, or blank for no limit\./,
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

test('transfer ownership: an admin is chosen in the row, then confirmed by typing the name', async ({ page }) => {
  // Transfer needs sign-in; the mock turns it on for this page's session.
  const res = await page.request.post('/__mock/reset', { data: { auth: { mode: 'session', signed_in: 'fresh' } } });
  expect(res.ok()).toBe(true);
  await page.goto('/o/acme/settings');

  await page.getByLabel('Transfer ownership').selectOption({ index: 0 });
  await page.getByRole('button', { name: 'Transfer', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Transfer ownership of Acme' });
  const confirm = dialog.getByRole('button', { name: 'Transfer ownership', exact: true });
  await expect(confirm).toBeDisabled();
  await dialog.getByLabel('Type Acme to confirm').fill('Acme');
  await expectAccessible(page);
  await confirm.click();
  await expectToast(page, 'You’re now an admin of Acme');
});

test('delete is enabled only by the typed name, then opens the next organization', async ({ page, request }) => {
  await page.goto('/o/acme/settings');
  await expect(
    page.getByText('Stops all 3 factories and deletes their definitions, items and logs. This can’t be undone.'),
  ).toBeVisible();
  const typed = page.getByLabel('Type Acme to confirm');
  const confirm = page.getByRole('button', { name: 'Delete organization', exact: true });
  await expect(confirm).toBeDisabled();
  await typed.fill('acme');
  await expect(confirm).toBeDisabled();
  await expectAccessible(page);
  await typed.fill('Acme');
  await expect(confirm).toBeEnabled();
  await confirm.click();

  await expect(page).toHaveURL(/\/o\/goal$/);
  await expectToast(page, 'Deleted Acme');
  expect((await request.get('/v1/orgs/acme')).status()).toBe(404);

  // The last organization: `/` says there is none.
  await page.goto('/o/goal/settings');
  await expect(
    page.getByText('Stops the factory and deletes their definitions, items and logs. This can’t be undone.'),
  ).toBeVisible();
  await page.getByLabel('Type Goal to confirm').fill('Goal');
  await page.getByRole('button', { name: 'Delete organization', exact: true }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'No organization yet' })).toBeVisible();
});
