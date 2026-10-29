// The account page at /account (docs/design/screens/account/account.jpg): the sections, the name, the notification
// email and table, personal tokens, and sign out everywhere. Not run in the box: Playwright needs a browser, which is
// not installed here. CI runs it against the mock.
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

test('the notification table shows the saved boxes, and a box keeps its change after a reload', async ({ page }) => {
  await signIn(page.request);
  await page.goto('/account');
  const digestEmail = page.getByRole('checkbox', { name: 'Daily digest at 08:00, Email' });
  await expect(digestEmail).toBeChecked();
  await expect(page.getByRole('row', { name: /Daily digest at 08:00/ }).getByText('None')).toBeVisible();

  const budgetSlack = page.getByRole('checkbox', { name: 'A budget passes 80%, Slack' });
  await expect(budgetSlack).not.toBeChecked();
  await expect(budgetSlack).not.toHaveAttribute('aria-disabled', 'true');
  const saved = page.waitForResponse((r) => r.url().endsWith('/v1/me/notifications') && r.request().method() === 'PUT');
  await budgetSlack.click();
  await saved;
  await page.reload();
  await expect(page.getByRole('checkbox', { name: 'A budget passes 80%, Slack' })).toBeChecked();
});

test('the Slack boxes are not available with no Slack connection, and the email is live', async ({ page }) => {
  await signIn(page.request);
  const gone = await page.request.delete('/v1/orgs/acme/connections/slack');
  expect(gone.ok()).toBeTruthy();
  await page.goto('/account');
  await expect(page.getByRole('checkbox', { name: 'An agent needs your OK for a write, Slack' })).toHaveAttribute(
    'aria-disabled',
    'true',
  );
  await expect(page.getByRole('textbox', { name: 'Email for notifications' })).toBeEnabled();
  await page.getByRole('button', { name: 'Sign out everywhere' }).click();
  await expect(page.getByRole('dialog', { name: 'Sign out of every browser and the CLI?' })).toBeVisible();
});

for (const width of [375, 768, 1440]) {
  test(`the notifications section fits at ${width} px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await signIn(page.request);
    await page.goto('/account#notifications');
    await expect(page.getByRole('heading', { level: 2, name: 'Notifications' })).toBeVisible();
    await expectNoHorizontalScroll(page);
  });
}
