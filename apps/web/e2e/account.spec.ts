// The account page at /account (docs/design/screens/account/account.jpg): the sections, the name, the
// disabled rows goald cannot serve yet, and sign out everywhere. Not run in the box: Playwright needs a
// browser, which is not installed here. CI runs it against the mock.
import { type APIRequestContext, expect, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll } from './support';

async function signIn(request: APIRequestContext) {
  const res = await request.post('/__mock/reset', {
    data: { auth: { mode: 'session', signed_in: 'fresh', email: 'f.cruz@example.com' } },
  });
  expect(res.ok()).toBeTruthy();
}

test('the account page lists its six sections in order, and keeps the name, theme and tokens live', async ({
  page,
}) => {
  await signIn(page.request);
  await page.goto('/account');
  await expect(page.getByRole('heading', { level: 1, name: 'Your account' })).toBeVisible();
  const nav = page.getByRole('navigation', { name: 'Account sections' });
  await expect(nav.getByRole('link')).toHaveText([
    'Profile',
    'Appearance',
    'Notifications',
    'Personal tokens',
    'Connected apps',
    'Sessions',
  ]);
  await expect(page.getByRole('textbox', { name: 'Name' })).toHaveValue('F. Cruz');
  await expect(page.getByRole('group', { name: 'Theme' }).getByRole('button', { name: 'Dark' })).toBeVisible();
  await expect(page.locator('#personal-tokens')).toBeVisible();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('a name outside 1 to 80 characters is refused, and a good one is saved', async ({ page }) => {
  await signIn(page.request);
  await page.goto('/account');
  const name = page.getByRole('textbox', { name: 'Name' });
  await name.fill('   ');
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.getByText('A name is 1 to 80 characters.')).toBeVisible();
  await name.fill('Freddy Cruz');
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.getByText('Name saved')).toBeVisible();
});

test('the rows goald cannot serve yet are disabled, and sign out everywhere asks first', async ({ page }) => {
  await signIn(page.request);
  await page.goto('/account');
  await expect(page.getByRole('textbox', { name: 'Email for notifications' })).toHaveAttribute('aria-disabled', 'true');
  await expect(page.getByRole('checkbox', { name: 'An agent needs your OK for a write, Slack' })).toHaveAttribute(
    'aria-disabled',
    'true',
  );
  await page.getByRole('button', { name: 'Sign out everywhere' }).click();
  await expect(page.getByRole('dialog', { name: 'Sign out of every browser and the CLI?' })).toBeVisible();
});
