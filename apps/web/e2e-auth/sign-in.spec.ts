// The one sign-in flow against the real thing (spec 3 §13, `task web:e2e-auth`): goald under
// GOAL_AUTH=session, the identity service and Mailpit. The bootstrap owner signs in with the code
// Mailpit received, goald answers as them, the session cookie stays out of reach of scripts, and
// signing out ends access at once.
import { type APIRequestContext, expect, test } from '@playwright/test';

const mailpit = process.env.GOAL_MAILPIT_URL ?? 'http://127.0.0.1:8025';
// Mailpit's API needs the user:password goal up generated (GOAL_MAILPIT_AUTH; the Go e2e passes it).
const mailpitAuth = process.env.GOAL_MAILPIT_AUTH;
const mailpitHeaders = mailpitAuth ? { authorization: `Basic ${btoa(mailpitAuth)}` } : {};
const email = process.env.GOAL_BOOTSTRAP_OWNER_EMAIL;

interface MailpitSearch {
  messages: { ID: string; Created: string }[];
}

/** The 6-digit code of the newest message to `to` received after `since`. */
async function codeFromMailpit(request: APIRequestContext, to: string, since: number): Promise<string> {
  let code = '';
  await expect
    .poll(
      async () => {
        const search = await request.get(`${mailpit}/api/v1/search`, {
          params: { query: `to:"${to}"`, limit: 5 },
          headers: mailpitHeaders,
        });
        const { messages } = (await search.json()) as MailpitSearch;
        const latest = messages.find((m) => Date.parse(m.Created) >= since);
        if (!latest) return '';
        const message = (await (
          await request.get(`${mailpit}/api/v1/message/${latest.ID}`, { headers: mailpitHeaders })
        ).json()) as {
          Text: string;
        };
        code = message.Text.match(/\b(\d{6})\b/)?.[1] ?? '';
        return code;
      },
      { timeout: 30_000, message: 'a sign-in code reaches Mailpit' },
    )
    .not.toBe('');
  return code;
}

test('the bootstrap owner signs in with an emailed code, and signing out ends access', async ({ page, request }) => {
  test.skip(!email, 'GOAL_BOOTSTRAP_OWNER_EMAIL names the bootstrap owner');
  const owner = email ?? '';

  const config = await (await request.get('/v1/auth/config')).json();
  expect(config.mode, 'goald runs with GOAL_AUTH=session').toBe('session');

  await page.goto('/');
  await expect(page).toHaveURL(/\/sign-in/);
  const since = Date.now() - 1000;
  await page.getByLabel('Email').fill(owner);
  await page.getByRole('button', { name: 'Email me a code' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Check your email' })).toBeVisible();

  await page.getByLabel('Code').fill(await codeFromMailpit(request, owner, since));
  await expect(page).toHaveURL(/\/o\/[^/]+$/);

  // goald answers as the owner; the session is in an httpOnly cookie only.
  const me = await page.request.get('/v1/me');
  expect(me.status()).toBe(200);
  expect((await me.json()).user.email).toBe(owner.toLowerCase());
  expect(await page.evaluate(() => document.cookie)).not.toContain('session_token');
  // Nor in what a page script can fetch (spec 3 §11.1 row 5, review M2).
  const [cookie] = (await page.context().cookies()).filter((c) => c.name.endsWith('session_token'));
  const token = decodeURIComponent(cookie?.value ?? '').split('.')[0] ?? '';
  expect(token).toMatch(/^[A-Za-z0-9]{32}$/);
  const answer = await page.evaluate(() => fetch('/api/auth/get-session').then((r) => r.text()));
  expect(answer).toContain(owner.toLowerCase());
  expect(answer).not.toContain(token);

  await page
    .getByRole('navigation', { name: 'Organization and factories' })
    .getByRole('button', { name: /, account$/ })
    .click();
  await page.getByRole('dialog', { name: 'Account' }).getByRole('button', { name: 'Sign out', exact: true }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Sign in to Goal' })).toBeVisible();
  expect((await page.request.get('/v1/me')).status()).toBe(401);
});
