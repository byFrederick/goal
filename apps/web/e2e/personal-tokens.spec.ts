// Personal tokens (spec 3 §10.2 "Personal tokens", "Members' tokens", PR 3-12b) at every width
// against the mock, signed in as F. Cruz: their own tokens and every member's, creating one from
// a link that tries to set it up (nothing is taken from the URL; the warning; the secret once),
// revoking one's own without a recent sign-in, creating with an older sign-in, an owner revoking
// a member's token, a member who sees only their own tokens, the account menu's link, and the
// page with sign-in off.
import { type APIRequestContext, expect, type Page, test } from '@playwright/test';
import {
  expectAccessible,
  expectNoHorizontalScroll,
  expectToast,
  isPhone,
  orgMarkOf,
  orgNav,
  recordToasts,
} from './support';

/** Signs in as F. Cruz, the owner (or `email`, a person of the mock's people.ts). */
async function signIn(request: APIRequestContext, auth: Record<string, unknown> = {}, signedIn = 'fresh') {
  const res = await request.post('/__mock/reset', {
    data: { auth: { mode: 'session', signed_in: signedIn, ...auth } },
  });
  expect(res.ok()).toBe(true);
}

test.beforeEach(async ({ page }) => {
  await recordToasts(page);
});

const own = (page: Page) => page.getByRole('region', { name: 'Personal tokens' });
const members = (page: Page) => page.getByRole('region', { name: 'Members’ tokens' });
const dialog = (page: Page) => page.getByRole('dialog', { name: 'New token' });
const created = (page: Page) => page.getByRole('dialog', { name: 'Token created' });

/** A token's row (≥ 960 px of content) or its card (narrower), in a section. */
const entry = (section: ReturnType<Page['locator']>, name: string) =>
  section.locator('tr:visible, li:visible').filter({ has: section.page().getByText(name, { exact: true }) });

test('your own tokens and every member’s are listed, and the page fits the screen', async ({ page }) => {
  await signIn(page.request);
  await page.goto('/o/acme/api-keys');
  await expect(own(page).getByRole('heading', { name: 'Personal tokens' })).toBeVisible();
  await expect(
    own(page).getByText(
      'A personal token lets the CLI or a script on your machine act as you in Acme, with no more than your role allows. Tokens expire after at most 90 days.',
    ),
  ).toBeVisible();
  const laptop = entry(own(page), 'laptop');
  await expect(laptop.getByText('goal_pat_Q7mk…')).toBeVisible();
  await expect(laptop.getByText('See factories')).toBeVisible();
  await expect(laptop.getByText('+2 more')).toBeVisible();
  // Another member's token is never among your own.
  await expect(entry(own(page), 'build-box')).toHaveCount(0);
  if (await own(page).locator('table').isVisible()) {
    await expect(entry(own(page), 'old-desktop').getByText(/Revoked .* by F\. Cruz/)).toBeVisible();
  } else {
    await expect(
      own(page).getByText('1 revoked token is hidden. Revoked tokens stay listed for 30 days.'),
    ).toBeVisible();
  }

  const box = entry(members(page), 'build-box');
  await expect(box.getByText('M. Okafor')).toBeVisible();
  await expect(box.getByText('in 5 days')).toBeVisible();
  await expect(entry(members(page), 'laptop').filter({ hasText: 'J. Lindqvist' })).toBeVisible();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('a new token takes nothing from the link, warns, and shows its secret once', async ({ page }) => {
  await signIn(page.request);
  // A link that tries to set a token up for someone else's use: the dialog ignores all of it.
  await page.goto('/o/acme/api-keys?name=evil&scopes=definition:publish&expires_in_days=90#personal-tokens');
  await expect(own(page).getByRole('heading', { name: 'Personal tokens' })).toBeFocused();
  await own(page).getByRole('button', { name: 'New token' }).click();
  const d = dialog(page);
  await expect(d.getByLabel('Name')).toBeFocused();
  await expect(d.getByLabel('Name')).toHaveValue('');
  await expect(d.getByLabel('Publish versions')).not.toBeChecked();
  await expect(d.getByLabel('See the organization')).not.toBeChecked();
  await expect(d.getByRole('radio', { name: '30 days' })).toBeChecked();
  await expect(
    d.getByText(
      'Publishing a version controls the prompts agents run and the repository they push to. Give this only to a token you keep safe.',
    ),
  ).toBeVisible();

  await d.getByLabel('Name').fill('build-laptop');
  await d.getByRole('button', { name: 'CLI: work' }).click();
  await expect(d.getByLabel('Submit items')).toBeChecked();
  await expect(d.getByLabel('Pull and clear the andon')).toBeChecked();
  // The preset never gives a high-impact scope.
  await expect(d.getByLabel('Edit drafts')).not.toBeChecked();
  await expect(d.getByLabel('Publish versions')).not.toBeChecked();
  await expect(d.getByLabel('Create factories')).not.toBeChecked();
  await expectAccessible(page);
  await d.getByRole('button', { name: 'Create token' }).click();

  const shown = created(page);
  await expect(shown.getByText('Copy your token now. You won’t see it again.')).toBeVisible();
  await expect(shown.getByText('Anyone with this token can act as you in Acme. Goal never asks for it.')).toBeVisible();
  const secret = (await shown.locator('code').textContent()) ?? '';
  expect(secret).toMatch(/^goal_pat_[A-Za-z0-9_-]{43}$/);
  const done = shown.getByRole('button', { name: 'Done' });
  await expect(done).toBeDisabled();
  await page.keyboard.press('Escape');
  await expect(shown).toBeVisible();
  await expectAccessible(page);
  await shown.getByLabel('I’ve stored it somewhere safe').check();
  await done.click();
  await expect(shown).toBeHidden();

  await expect(entry(own(page), 'build-laptop').getByText(`${secret.slice(0, 13)}…`)).toBeVisible();
  await expect(page.getByText(secret)).toHaveCount(0);
});

test('a token needs a name and something to do, and a live token’s name is taken', async ({ page }) => {
  await signIn(page.request);
  await page.goto('/o/acme/api-keys');
  await own(page).getByRole('button', { name: 'New token' }).click();
  const d = dialog(page);
  await d.getByRole('button', { name: 'Create token' }).click();
  await expect(d.getByText('Give the token a name')).toBeVisible();
  await expect(d.getByText('Choose at least one thing it can do')).toBeVisible();

  await d.getByLabel('Name').fill('Laptop');
  await d.getByLabel('See the organization').check();
  await d.getByRole('button', { name: 'Create token' }).click();
  await expect(d.getByText('Another live token of yours has this name')).toBeVisible();
  await expect(created(page)).toHaveCount(0);
});

test('revoking your own token needs no recent sign-in', async ({ page }) => {
  await signIn(page.request, {}, 'stale');
  await page.goto('/o/acme/api-keys');
  await entry(own(page), 'laptop').getByRole('button', { name: 'Revoke laptop…' }).click();
  const confirm = page.getByRole('dialog', { name: 'Revoke laptop?' });
  await expect(confirm.getByText('Anything using it stops working at once.')).toBeVisible();
  await expect(confirm.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await confirm.getByRole('button', { name: 'Revoke token' }).click();

  await expectToast(page, 'Revoked laptop');
  await expect(page.getByRole('dialog', { name: 'Confirm it’s you' })).toHaveCount(0);
  await expect(own(page).getByRole('button', { name: 'Revoke laptop…' })).toHaveCount(0);
});

test('creating a token with an older sign-in confirms it’s you, then creates it', async ({ page }) => {
  await signIn(page.request, {}, 'stale');
  await page.goto('/o/acme/api-keys');
  await own(page).getByRole('button', { name: 'New token' }).click();
  const d = dialog(page);
  await d.getByLabel('Name').fill('headless');
  await d.getByLabel('See factories').check();
  await d.getByRole('button', { name: 'Create token' }).click();

  const reauth = page.getByRole('dialog', { name: 'Confirm it’s you' });
  await reauth.getByRole('button', { name: 'Email me a code' }).click();
  await reauth.getByLabel('Code').fill('000000');
  await expect(reauth).toBeHidden();
  await expect(created(page)).toBeVisible();
});

test('an owner revokes a member’s token, after confirming', async ({ page }) => {
  await signIn(page.request);
  await page.goto('/o/acme/api-keys');
  await entry(members(page), 'build-box').getByRole('button', { name: 'Revoke M. Okafor’s token build-box…' }).click();
  const confirm = page.getByRole('dialog', { name: 'Revoke M. Okafor’s token build-box?' });
  await expect(confirm.getByText('Anything using it stops working at once.')).toBeVisible();
  await confirm.getByRole('button', { name: 'Revoke token' }).click();
  await expectToast(page, 'Revoked M. Okafor’s token build-box');
  await expect(members(page).getByRole('button', { name: 'Revoke M. Okafor’s token build-box…' })).toHaveCount(0);
});

test('a member without api_key:read sees only their own tokens', async ({ page }) => {
  // J. Lindqvist, a member of Acme with factory roles on sre and support (people.ts).
  await signIn(page.request, { email: 'j.lindqvist@example.com' });
  await page.goto('/o/acme');
  if (!isPhone(page)) {
    await orgMarkOf(page).click();
    await page.getByRole('menuitem', { name: 'API keys' }).click();
  } else {
    await page.goto('/o/acme/api-keys');
  }
  await expect(page).toHaveURL(/\/o\/acme\/api-keys$/);
  await expect(own(page).getByRole('heading', { name: 'Personal tokens' })).toBeVisible();
  await expect(entry(own(page), 'laptop').getByText('goal_pat_b4Ty…')).toBeVisible();
  // F. Cruz's tokens are not hers.
  await expect(entry(own(page), 'old-desktop')).toHaveCount(0);
  await expect(members(page)).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'New key' })).toHaveCount(0);
  await expect(page.getByText('Use a key from CI')).toHaveCount(0);

  // A member can give a token only what they hold: members do not create factories.
  await own(page).getByRole('button', { name: 'New token' }).click();
  const d = dialog(page);
  await expect(d.getByLabel('See the organization')).toBeEnabled();
  await expect(d.getByLabel('Create factories')).toBeDisabled();
  await expect(d.getByLabel('Create factories')).toHaveAccessibleDescription('Your role doesn’t allow it');
  await expectAccessible(page);
});

test('the account menu opens your personal tokens', async ({ page }) => {
  await signIn(page.request);
  await page.goto('/o/acme');
  if (isPhone(page)) {
    await orgNav(page)
      .getByRole('button', { name: /switch organization/ })
      .click();
    await page.getByRole('region', { name: 'Account' }).getByRole('link', { name: 'Personal tokens' }).click();
  } else {
    await orgNav(page).getByRole('button', { name: 'FC, F. Cruz, account' }).click();
    await page.getByRole('dialog', { name: 'Account' }).getByRole('link', { name: 'Personal tokens' }).click();
  }
  await expect(page).toHaveURL(/\/o\/acme\/api-keys#personal-tokens$/);
  await expect(own(page).getByRole('heading', { name: 'Personal tokens' })).toBeFocused();
  await expect(own(page).getByRole('heading', { name: 'Personal tokens' })).toBeInViewport();
});

test('with sign-in off there are no personal tokens to make', async ({ page, request }) => {
  await request.post('/__mock/reset');
  await page.goto('/o/acme/api-keys');
  await expect(
    own(page).getByText('Personal tokens need sign-in. With sign-in off, the CLI acts as the local user without one.'),
  ).toBeVisible();
  await expect(own(page).getByRole('button', { name: 'New token' })).toHaveCount(0);
});
