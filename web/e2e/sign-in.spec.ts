// Web sign-in (spec 3 §10, PR 3-10) at every width against the mock with sign-in on: the email
// code, its states, Google's round trip, returning to the page that asked, the account, signing
// out (here and everywhere), and confirming it's you before a change that needs a recent sign-in.
import { type APIRequestContext, expect, type Locator, type Page, test } from '@playwright/test';
import {
  expectAccessible,
  expectNoHorizontalScroll,
  expectToast,
  isPhone,
  orgMarkOf,
  orgNav,
  recordToasts,
} from './support';

const owner = 'f.cruz@example.com';

interface AuthReset {
  signed_in?: 'fresh' | 'stale';
  email?: string;
  google?: 'ok' | 'not-invited' | 'unverified' | 'failed' | 'off';
  platform_admin?: false;
}

/** Turns sign-in on. Signed in: the reset goes through the page's own request context, so its cookie lands in the browser. */
async function signInOn(request: APIRequestContext, auth: AuthReset = {}) {
  const res = await request.post('/__mock/reset', { data: { auth: { mode: 'session', ...auth } } });
  expect(res.ok()).toBe(true);
}

const fail = (request: APIRequestContext, fault: Record<string, unknown>) =>
  request.post('/__mock/fail', { data: fault });

/**
 * No script on the page can read the session token: not from the cookie (httpOnly), nor from what
 * it fetches, get-session's answer included (spec 3 §11.1 row 5, review M2).
 */
async function expectNoSessionTokenInScripts(page: Page) {
  const [cookie] = (await page.context().cookies()).filter((c) => c.name === 'goal.session_token');
  const token = decodeURIComponent(cookie?.value ?? '').split('.')[0] ?? '';
  expect(token).toMatch(/^[A-Za-z0-9]{32}$/);
  const answer = await page.evaluate(() => fetch('/api/auth/get-session').then((r) => r.text()));
  expect(answer).toContain('"user"');
  expect(answer).not.toContain(token);
}

const emailField = (page: Page) => page.getByLabel('Email');
const codeField = (page: Page) => page.getByLabel('Code');
const sendCode = (page: Page) => page.getByRole('button', { name: 'Email me a code' });

async function askForCode(page: Page, email = owner) {
  await emailField(page).fill(email);
  await sendCode(page).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Check your email' })).toBeVisible();
}

test.beforeEach(async ({ page }) => {
  await recordToasts(page);
});

test('signing in by code returns to the page that asked, and the session stays out of reach of scripts', async ({
  page,
  request,
}) => {
  await signInOn(request);
  await page.goto('/o/acme/f/goal/board');

  // goald answers 401: the sign-in page opens, remembering where the person was.
  await expect(page.getByRole('heading', { level: 1, name: 'Sign in to Goal' })).toBeVisible();
  await expect(page).toHaveURL(/\/sign-in\?next=%2Fo%2Facme%2Ff%2Fgoal%2Fboard$/);
  await expect(page).toHaveTitle('Sign in · Goal');
  await expect(page.getByText('Goal is invite-only. Ask an owner of your organization to invite you.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Continue with Google' })).toBeVisible();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);

  // An address that is not one says so, at the field.
  await emailField(page).fill('f.cruz');
  await sendCode(page).click();
  await expect(emailField(page)).toHaveAccessibleDescription('Enter your email address, like you@example.com.');

  await askForCode(page);
  // The address travels in the page's state, never in the URL.
  expect(page.url()).not.toContain('cruz');
  await expect(page.getByText(`We sent a 6-digit code to ${owner}. It expires in 5 minutes.`)).toBeVisible();
  await expect(codeField(page)).toBeFocused();
  await expect(codeField(page)).toHaveAttribute('autocomplete', 'one-time-code');
  await expect(codeField(page)).toHaveAttribute('inputmode', 'numeric');
  await expect(page.getByRole('button', { name: /^Send a new code in 0:[23]\d$/ })).toBeDisabled();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);

  // The sixth digit sends the code; the app asks for the session to keep it alive.
  const keptAlive = page.waitForRequest((r) => r.url().endsWith('/api/auth/get-session'));
  await codeField(page).fill('000 000');
  await expect(page).toHaveURL(/\/o\/acme\/f\/goal\/board$/);
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
  await keptAlive;
  expect(await page.evaluate(() => document.cookie)).not.toContain('session_token');
  await expectNoSessionTokenInScripts(page);

  // The account names the person, their role here and that they are a platform admin.
  await orgNav(page).getByRole('button', { name: 'FC, F. Cruz, account' }).click();
  const account = page.getByRole('dialog', { name: 'Account' });
  await expect(account.getByText('F. Cruz', { exact: true })).toBeVisible();
  await expect(account.getByText(owner)).toBeVisible();
  await expect(account.getByText('Owner of Acme')).toBeVisible();
  await expect(account.getByText('Platform admin')).toBeVisible();
  await expect(account.getByRole('button', { name: 'Edit name…' })).toBeVisible();
  await expectAccessible(page);
  await page.keyboard.press('Escape');
  await expect(account).toBeHidden();
});

test('a wrong code, a used-up code, a new code after the countdown, an expired one', async ({ page, request }) => {
  await signInOn(request);
  await page.clock.install();
  await page.goto('/sign-in');
  await askForCode(page);

  const message = (text: string) => page.getByText(text, { exact: true });
  await codeField(page).fill('111111');
  await expect(message('That code didn’t work. Check it, or send a new one.')).toBeVisible();
  await expect(codeField(page)).toHaveAttribute('aria-invalid', 'true');
  await expect(codeField(page)).toHaveAccessibleDescription('That code didn’t work. Check it, or send a new one.');
  await expectAccessible(page);
  await codeField(page).fill('222222');
  await expect(message('That code didn’t work. Check it, or send a new one.')).toBeVisible();
  await codeField(page).fill('333333');

  // Three wrong tries void the code: only a new one helps, and it can be asked for at once.
  await expect(message('Send a new code to try again.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Sign in' })).toBeDisabled();
  await expect(codeField(page)).toBeDisabled();
  await page.getByRole('button', { name: 'Send a new code' }).click();
  await expect(message('We sent a new code.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Send a new code in 0:30' })).toBeDisabled();

  // The countdown runs out: another code may be asked for.
  await page.clock.runFor(30_000);
  await expect(page.getByRole('button', { name: 'Send a new code', exact: true })).toBeEnabled();

  // Codes last 5 minutes.
  await fail(request, { path: '/api/auth/sign-in/email-otp', status: 400, code: 'OTP_EXPIRED' });
  await codeField(page).fill('000000');
  await expect(message('That code has expired. Send a new one.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Send a new code', exact: true })).toBeEnabled();

  await codeField(page).fill('');
  await codeField(page).fill('000000');
  await expect(page).toHaveURL(/\/o\/acme$/);

  // "Use a different email" goes back with the address left behind.
  await page.goto('/sign-in');
  await askForCode(page);
  await page.getByRole('link', { name: 'Use a different email' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Sign in to Goal' })).toBeVisible();
});

test('the sign-in page says what failed: Goal unreachable, too many tries, Google', async ({ page, request }) => {
  await signInOn(request);
  await page.clock.install();
  await page.goto('/sign-in');

  await fail(request, { path: '/api/auth/email-otp/send-verification-otp', status: 503, code: 'auth-unavailable' });
  await emailField(page).fill(owner);
  await sendCode(page).click();
  await expect(page.getByText('Can’t reach Goal. Check your connection and try again.')).toBeVisible();

  await fail(request, { path: '/api/auth/email-otp/send-verification-otp', status: 429 });
  await sendCode(page).click();
  await expect(page.getByText('Too many tries. Try again in 2 minutes.')).toBeVisible();
  await expect(sendCode(page)).toBeDisabled();
  await expectAccessible(page);
  // The wait counts down; once over, the message goes and the button works again.
  await page.clock.runFor(61_000);
  await expect(page.getByText('Too many tries. Try again in 1 minute.')).toBeVisible();
  await page.clock.runFor(60_000);
  await expect(page.getByText(/^Too many tries/)).toHaveCount(0);
  await expect(sendCode(page)).toBeEnabled();

  await page.goto('/sign-in?error=SIGN_UP_NOT_ALLOWED&error_description=Only%20invited%20people%20can%20sign%20up');
  await expect(page.getByText('That Google account isn’t invited to Goal.')).toBeVisible();
  await page.goto('/sign-in?error=unable_to_get_user_info');
  await expect(
    page.getByText('Google didn’t share a verified email for that account. Sign in with an email code instead.'),
  ).toBeVisible();
  await page.goto('/sign-in?error=access_denied');
  await expect(page.getByText('Google sign-in didn’t finish. Try again.')).toBeVisible();
  await expectAccessible(page);

  // Only same-origin paths are followed after signing in.
  await page.goto(`/sign-in?next=${encodeURIComponent('https://evil.example.com/')}`);
  await askForCode(page);
  await codeField(page).fill('000000');
  await expect(page).toHaveURL(/^http:\/\/127\.0\.0\.1:\d+\/o\/acme$/);
});

test('Continue with Google returns to the page that asked; an account without an invitation is told so', async ({
  page,
  request,
}) => {
  await signInOn(request);
  await page.goto('/o/acme/settings');
  await page.getByRole('button', { name: 'Continue with Google' }).click();
  await expect(page).toHaveURL(/\/o\/acme\/settings$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Organization settings' })).toBeVisible();

  await signInOn(request, { google: 'not-invited' });
  await page.context().clearCookies();
  await page.goto('/o/acme/settings');
  await page.getByRole('button', { name: 'Continue with Google' }).click();
  await expect(page.getByText('That Google account isn’t invited to Goal.')).toBeVisible();
  await expect(page).toHaveURL(/\/sign-in\?next=%2Fo%2Facme%2Fsettings&error=SIGN_UP_NOT_ALLOWED&error_description=/);

  // Google off: no button.
  await signInOn(request, { google: 'off' });
  await page.goto('/sign-in');
  await expect(sendCode(page)).toBeVisible();
  await expect(page.getByRole('button', { name: 'Continue with Google' })).toHaveCount(0);
});

test('an address that may not sign in gets the same answer, and no code works for it', async ({ page, request }) => {
  await signInOn(request);
  await page.goto('/sign-in');
  await askForCode(page, 'stranger@example.com');
  await codeField(page).fill('000000');
  await expect(page.getByText('That code didn’t work. Check it, or send a new one.', { exact: true })).toBeVisible();
});

test('signed in to no organization, the person can only sign out', async ({ page, request }) => {
  await signInOn(request);
  await page.goto('/sign-in');
  await askForCode(page, 'j.doe@example.com');
  await codeField(page).fill('000000');

  await expect(page.getByRole('heading', { level: 1, name: 'Not in an organization yet' })).toBeVisible();
  await expect(
    page.getByText(
      'You’re signed in as j.doe@example.com but not in an organization yet. Ask an owner for an invitation.',
    ),
  ).toBeVisible();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
  await page.getByRole('button', { name: 'Sign out' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Sign in to Goal' })).toBeVisible();
});

test('edit name, sign out everywhere, sign out', async ({ page }) => {
  await signInOn(page.request, { signed_in: 'fresh' });
  await page.goto('/o/acme');

  const openAccount = async (name: string) => {
    await orgNav(page)
      .getByRole('button', { name: `${name}, account` })
      .click();
    return page.getByRole('dialog', { name: 'Account' });
  };

  await (await openAccount('FC, F. Cruz')).getByRole('button', { name: 'Edit name…' }).click();
  const edit = page.getByRole('dialog', { name: 'Edit name' });
  await expect(edit.getByLabel('Name')).toBeFocused();
  await expect(edit.getByLabel('Name')).toHaveValue('F. Cruz');
  await expect(edit.getByText('1 to 80 characters. Acme’s members and the audit log show it.')).toBeVisible();
  await edit.getByLabel('Name').fill('  ');
  await edit.getByRole('button', { name: 'Save' }).click();
  await expect(edit.getByLabel('Name')).toHaveAccessibleDescription(/^A name is 1 to 80 characters\./);
  await expectAccessible(page);
  await edit.getByLabel('Name').fill('Fred Cruz');
  await edit.getByRole('button', { name: 'Save' }).click();
  await expect(edit).toBeHidden();
  await expectToast(page, 'Name saved');

  // Sign out everywhere asks first; Cancel has focus, so Enter never signs anyone out by accident.
  await (await openAccount('FC, Fred Cruz')).getByRole('button', { name: 'Sign out everywhere…' }).click();
  const confirm = page.getByRole('dialog', { name: 'Sign out of every browser and the CLI?' });
  await expect(
    confirm.getByText(
      'Every session ends, this one too. API keys and personal tokens keep working until they are revoked.',
    ),
  ).toBeVisible();
  await expect(confirm.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await expectAccessible(page);
  await confirm.getByRole('button', { name: 'Sign out everywhere' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Sign in to Goal' })).toBeVisible();

  // Every session ended: the app asks for sign-in again.
  await page.goto('/o/acme');
  await expect(page).toHaveURL(/\/sign-in\?next=%2Fo%2Facme$/);

  // Sign out (here only).
  await signInOn(page.request, { signed_in: 'fresh' });
  await page.goto('/o/acme');
  if (isPhone(page)) {
    // On a phone the account is also at the bottom of the organization sheet.
    await orgNav(page)
      .getByRole('button', { name: /switch organization/ })
      .click();
    const block = page.getByRole('region', { name: 'Account' });
    await expect(block.getByText('f.cruz@example.com, owner of Acme')).toBeVisible();
    await block.getByRole('button', { name: 'Sign out', exact: true }).click();
  } else {
    await (await openAccount('FC, F. Cruz')).getByRole('button', { name: 'Sign out', exact: true }).click();
  }
  await expect(page.getByRole('heading', { level: 1, name: 'Sign in to Goal' })).toBeVisible();
  await page.goto('/o/acme');
  await expect(page).toHaveURL(/\/sign-in\?next=%2Fo%2Facme$/);
});

test('a change that needs a recent sign-in asks to confirm it’s you, then goes through', async ({ page }) => {
  await signInOn(page.request, { signed_in: 'stale' });
  await page.goto('/o/acme/settings');
  await page.getByRole('button', { name: 'Delete organization…' }).click();
  await page.getByLabel('Type Acme to confirm').fill('Acme');
  await page.getByRole('button', { name: 'Delete organization', exact: true }).click();

  // goald answers 403 reauth-required: the dialog opens over the page, which keeps its state.
  const reauth = page.getByRole('dialog', { name: 'Confirm it’s you' });
  await expect(reauth.getByText(`This change needs a recent sign-in. We’ll email a code to ${owner}.`)).toBeVisible();
  await expect(reauth.getByRole('button', { name: 'Continue with Google' })).toBeVisible();
  await expectAccessible(page);
  await reauth.getByRole('button', { name: 'Email me a code' }).click();
  await expect(reauth.getByText(`We sent a 6-digit code to ${owner}.`)).toBeVisible();
  await expect(reauth.getByLabel('Code')).toBeFocused();
  await expectAccessible(page);
  await reauth.getByLabel('Code').fill('000000');

  // Signed in again, the deletion is sent once more and goes through.
  await expect(reauth).toBeHidden();
  await expect(page).toHaveURL(/\/o\/example-labs$/);
  await expectToast(page, 'Deleted Acme');
});

test('cancelling the confirmation leaves the change undone', async ({ page }) => {
  await signInOn(page.request, { signed_in: 'stale' });
  await page.goto('/o/acme/settings');
  await page.getByRole('button', { name: 'Delete organization…' }).click();
  await page.getByLabel('Type Acme to confirm').fill('Acme');
  await page.getByRole('button', { name: 'Delete organization', exact: true }).click();
  const reauth = page.getByRole('dialog', { name: 'Confirm it’s you' });
  await reauth.getByRole('button', { name: 'Cancel' }).click();
  await expect(reauth).toBeHidden();
  await expect(page.getByRole('dialog', { name: 'Delete Acme?' })).toBeVisible();
  expect((await page.request.get('/v1/orgs/acme')).status()).toBe(200);
});

/** Opens the organization menu, or the phone's sheet, and returns it. */
async function openOrgMenu(page: Page) {
  await orgMarkOf(page).click();
  const menu = isPhone(page) ? page.getByRole('dialog', { name: 'Switch organization' }) : page.getByRole('menu');
  await expect(menu.getByRole(isPhone(page) ? 'link' : 'menuitem', { name: 'Organization settings' })).toBeVisible();
  return menu;
}

const createOrg = (page: Page, menu: Locator) =>
  menu.getByRole(isPhone(page) ? 'button' : 'menuitem', { name: 'Create organization' });

test('only platform admins are offered Create organization, and goald refuses anyone else', async ({ page }) => {
  await signInOn(page.request, { signed_in: 'fresh', platform_admin: false });
  await page.goto('/o/acme');
  const menu = await openOrgMenu(page);
  await expect(createOrg(page, menu)).toHaveCount(0);
  await expectAccessible(page);
  const refused = await page.request.post('/v1/orgs', { data: { name: 'Acme Two', slug: 'acme-two' } });
  expect(refused.status()).toBe(403);

  await signInOn(page.request, { signed_in: 'fresh' });
  await page.goto('/o/acme');
  await expect(createOrg(page, await openOrgMenu(page))).toBeVisible();
});

test('with sign-in off there is no sign-in page', async ({ page, request }) => {
  await request.post('/__mock/reset');
  await page.goto('/sign-in?next=%2Fo%2Facme%2Fsettings');
  await expect(page).toHaveURL(/\/o\/acme\/settings$/);
  await page.goto('/sign-in');
  await expect(page).toHaveURL(/\/o\/acme$/);
});

test('an invitation link asks a signed-out person to sign in, then comes back to it', async ({ page, request }) => {
  await signInOn(request);
  await page.goto('/invite/2f0c6d8e-1111-4a2b-9c3d-000000000001');
  await expect(page.getByRole('heading', { level: 1, name: 'Sign in to see your invitation' })).toBeVisible();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
  await page.getByRole('link', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/sign-in\?next=%2Finvite%2F2f0c6d8e-1111-4a2b-9c3d-000000000001$/);
});
