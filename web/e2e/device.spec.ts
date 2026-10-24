// Device-Approve (spec 3 §10.2, PR 3-13) at every width against the mock with sign-in on: goal
// login's code is typed, never taken from the URL; the page shows who asks and from where;
// approving needs a recent sign-in; the CLI's poll then gets its session, or is told it was denied.
import { type APIRequestContext, expect, type Page, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll } from './support';

const owner = 'f.cruz@example.com';
const cliAgent = 'goal/1.4 (darwin)';

interface DeviceCode {
  device_code: string;
  user_code: string;
}

/** What goal login does first: ask for a code as the Goal CLI (from another network with `ip`). */
async function cliAsksForACode(request: APIRequestContext, ip?: string): Promise<DeviceCode> {
  const res = await request.post('/api/auth/device/code', {
    data: { client_id: 'goal-cli' },
    headers: { 'User-Agent': cliAgent, ...(ip ? { 'X-Mock-Client-Ip': ip } : {}) },
  });
  expect(res.ok()).toBe(true);
  return (await res.json()) as DeviceCode;
}

/** One poll of goal login: the session's token, or the OAuth error. */
async function cliPolls(request: APIRequestContext, code: DeviceCode): Promise<string> {
  const res = await request.post('/api/auth/device/token', {
    data: {
      grant_type: 'urn:ietf:params:oauth:grant-type:device_code',
      device_code: code.device_code,
      client_id: 'goal-cli',
    },
  });
  const body = (await res.json()) as { access_token?: string; error?: string };
  return body.access_token ? 'signed in' : (body.error ?? 'nothing');
}

async function signInOn(page: Page, signedIn?: 'fresh' | 'stale') {
  const res = await page.request.post('/__mock/reset', {
    data: { auth: { mode: 'session', ...(signedIn ? { signed_in: signedIn } : {}) } },
  });
  expect(res.ok()).toBe(true);
}

const codeField = (page: Page) => page.getByLabel('Code', { exact: true });
const display = (code: DeviceCode) => `${code.user_code.slice(0, 4)}-${code.user_code.slice(4)}`;

test('signed out, the person signs in first; the code in the link is never used, only the one typed', async ({
  page,
  request,
}) => {
  await signInOn(page);
  const code = await cliAsksForACode(request);

  // A link with the code in it (verification_uri_complete) opens the page like any other.
  await page.goto(`/device?user_code=${code.user_code}`);
  await expect(page.getByRole('heading', { level: 1, name: 'Sign in to approve the Goal CLI' })).toBeVisible();
  await expect(page).toHaveTitle('Sign in to the Goal CLI · Goal');
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
  await page.getByRole('link', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/sign-in\?next=%2Fdevice$/);

  await page.getByLabel('Email').fill(owner);
  await page.getByRole('button', { name: 'Email me a code' }).click();
  await page.getByLabel('Code').fill('000000');

  // Back on the page, nothing was filled in or decided from the link.
  await expect(page.getByRole('heading', { level: 1, name: 'Sign in to the Goal CLI' })).toBeVisible();
  await expect(page).toHaveURL(/\/device$/);
  await expect(codeField(page)).toHaveValue('');
  await expect(codeField(page)).toBeFocused();
  await expect(codeField(page)).toHaveAttribute('autocomplete', 'off');
  expect(await cliPolls(request, code)).toBe('authorization_pending');
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);

  // Typed in lower case without the dash, it reads as the terminal shows it.
  await codeField(page).fill(code.user_code.toLowerCase());
  await expect(codeField(page)).toHaveValue(display(code));
  await page.getByRole('button', { name: 'Continue' }).click();

  await expect(page.getByText(`Goal CLI wants to sign in as F. Cruz (${owner})`)).toBeVisible();
  await expect(page.getByText(display(code), { exact: true })).toBeVisible();
  await expect(page.getByText('Same network as this browser')).toBeVisible();
  await expect(page.getByText(cliAgent)).toBeVisible();
  await expect(page.getByText(/^just now, expires in 9 minutes$/)).toBeVisible();
  await expect(
    page.getByText('Only approve if you just ran goal login yourself. Never approve a code someone sent you.'),
  ).toBeVisible();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);

  await page.getByRole('button', { name: 'Approve' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'The CLI is signed in' })).toBeFocused();
  await expect(page.getByText('Approved for F. Cruz')).toBeVisible();
  await expect(page.getByText('You can close this tab.')).toBeVisible();
  await expectAccessible(page);
  expect(await cliPolls(request, code)).toBe('signed in');
});

test('approving with an older session asks to confirm it’s you first; a CLI elsewhere says so', async ({
  page,
  request,
}) => {
  await signInOn(page, 'stale');
  const code = await cliAsksForACode(request, '203.0.113.7');

  await page.goto('/device');
  await codeField(page).fill(code.user_code);
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page.getByText('203.0.113.7')).toBeVisible();
  await expect(page.getByText('A different network')).toBeVisible();

  await page.getByRole('button', { name: 'Approve' }).click();
  const reauth = page.getByRole('dialog', { name: 'Confirm it’s you' });
  await expect(reauth.getByText(`This change needs a recent sign-in. We’ll email a code to ${owner}.`)).toBeVisible();
  expect(await cliPolls(request, code)).toBe('authorization_pending');
  await reauth.getByRole('button', { name: 'Email me a code' }).click();
  await reauth.getByLabel('Code').fill('000000');

  // Signed in again, the approval is sent once more and goes through.
  await expect(reauth).toBeHidden();
  await expect(page.getByRole('heading', { level: 1, name: 'The CLI is signed in' })).toBeVisible();
  expect(await cliPolls(request, code)).toBe('signed in');
});

test('a code that is short or not valid is refused; Deny tells the CLI', async ({ page, request }) => {
  await signInOn(page, 'fresh');
  const code = await cliAsksForACode(request);
  await page.goto('/device');

  await codeField(page).fill('ABCD');
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(codeField(page)).toHaveAccessibleDescription(
    'Enter the 8-character code shown in your terminal, like ABCD-EFGH.',
  );

  await codeField(page).fill('ZZZZ-ZZZZ');
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page.getByText('That code isn’t valid any more. Run goal login again.')).toBeVisible();
  await expect(codeField(page)).toHaveAttribute('aria-invalid', 'true');
  await expect(codeField(page)).toHaveAccessibleDescription('That code isn’t valid any more. Run goal login again.');
  await expectAccessible(page);

  await codeField(page).fill(code.user_code);
  await page.getByRole('button', { name: 'Continue' }).click();
  await page.getByRole('button', { name: 'Deny' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Denied' })).toBeFocused();
  await expect(page.getByText('The CLI was not signed in.')).toBeVisible();
  expect(await cliPolls(request, code)).toBe('access_denied');

  // Once decided, the code is gone.
  await page.goto('/device');
  await codeField(page).fill(code.user_code);
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page.getByText('That code isn’t valid any more. Run goal login again.')).toBeVisible();
});

test('with sign-in off there is no device page', async ({ page, request }) => {
  await request.post('/__mock/reset');
  await page.goto('/device');
  await expect(page).toHaveURL(/\/o\/acme$/);
});
