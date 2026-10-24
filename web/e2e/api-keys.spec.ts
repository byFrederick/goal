// API keys (spec 3 §10.2 "Api-Keys", PR 3-12) at every width against the mock: the list, creating
// a key and its secret shown once, refusals, revoking, a list that fails to load, the empty
// organization, and confirming it's you before creating a key with an older sign-in.
import { expect, type Page, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll, expectToast, isPhone, recordToasts } from './support';

test.beforeEach(async ({ page, request }) => {
  await request.post('/__mock/reset');
  await recordToasts(page);
});

const heading = (page: Page) => page.getByRole('heading', { level: 1, name: 'API keys' });
const newKey = (page: Page) => page.getByRole('button', { name: 'New key' }).first();
const dialog = (page: Page) => page.getByRole('dialog', { name: 'New key' });
const created = (page: Page) => page.getByRole('dialog', { name: 'Key created' });

/** The key's row (≥ 960 px of content) or its card (narrower). */
const keyEntry = (page: Page, name: string) =>
  page.locator('tr:visible, li:visible').filter({ has: page.getByText(name, { exact: true }) });

test('the keys are listed with what each can do, and the page fits the screen', async ({ page }) => {
  await page.goto('/o/acme/api-keys');
  await expect(heading(page)).toBeVisible();
  await expect(page).toHaveTitle('API keys · Acme · Goal');
  await expect(page.getByRole('heading', { name: '3 active keys' })).toBeVisible();

  const ci = keyEntry(page, 'ci-deploy');
  await expect(ci.getByText('goal_sk_Ab3x…')).toBeVisible();
  await expect(ci.getByText('Submit items')).toBeVisible();
  await expect(ci.getByText('+1 more')).toBeVisible();
  await expect(keyEntry(page, 'nightly-evals').getByText('in 9 days')).toBeVisible();
  await expect(keyEntry(page, 'definitions-sync').getByText(/never/i)).toBeVisible();

  // Revoked keys: greyed in the table, counted on narrow screens.
  if (await page.getByRole('table', { name: 'API keys of Acme' }).isVisible()) {
    await expect(keyEntry(page, 'support-bot').getByText(/Revoked .* by F\. Cruz/)).toBeVisible();
  } else {
    await expect(page.getByText('1 revoked key is hidden. Revoked keys stay listed for 30 days.')).toBeVisible();
  }
  await expect(page.getByText('What a key can never do')).toBeVisible();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('the organization sidebar opens API keys', async ({ page }) => {
  test.skip(isPhone(page), 'sidebars are the 768 px and wider navigation');
  await page.goto('/o/acme');
  const sidebar = page.getByRole('navigation', { name: 'Acme organization' });
  await sidebar.getByRole('link', { name: 'API keys' }).click();
  await expect(page).toHaveURL(/\/o\/acme\/api-keys$/);
  await expect(heading(page)).toBeFocused();
});

test('a new key shows its secret once, then lists only its hint', async ({ page }) => {
  await page.goto('/o/acme/api-keys');
  await newKey(page).click();
  const d = dialog(page);
  await expect(d.getByLabel('Name')).toBeFocused();
  await d.getByLabel('Name').fill('release-pipeline');
  await expect(d.getByText('The audit log shows its actions as release-pipeline (API key).')).toBeVisible();
  await d.getByLabel('Only these').check();
  await d.getByLabel('goal', { exact: true }).check();
  await d.getByText('CI: submit work').click();
  await expect(d.getByLabel('See factories')).toBeChecked();
  await expect(d.getByLabel('Submit items')).toBeChecked();
  // An approver ceiling cannot publish: the scope says what it needs.
  await expect(d.getByLabel('Publish versions')).toBeDisabled();
  await expect(d.getByLabel('Publish versions')).toHaveAccessibleDescription('Needs Editor');
  await expectAccessible(page);
  await d.getByRole('button', { name: 'Create key' }).click();

  const shown = created(page);
  await expect(shown.getByText('Copy your key now. You won’t see it again.')).toBeVisible();
  const secret = (await shown.locator('code').textContent()) ?? '';
  expect(secret).toMatch(/^goal_sk_[A-Za-z0-9_-]{43}$/);
  const done = shown.getByRole('button', { name: 'Done' });
  await expect(done).toBeDisabled();
  await page.keyboard.press('Escape');
  await expect(shown).toBeVisible();
  await expectAccessible(page);
  await shown.getByLabel('I’ve stored it somewhere safe').check();
  await done.click();
  await expect(shown).toBeHidden();

  await expect(page.getByRole('heading', { name: '4 active keys' })).toBeVisible();
  await expect(keyEntry(page, 'release-pipeline').getByText(`${secret.slice(0, 12)}…`)).toBeVisible();
  await expect(page.getByText(secret)).toHaveCount(0);
});

test('a key needs a name and something to do, and a live key’s name is taken', async ({ page }) => {
  await page.goto('/o/acme/api-keys');
  await newKey(page).click();
  const d = dialog(page);
  await d.getByRole('button', { name: 'Create key' }).click();
  await expect(d.getByText('Give the key a name')).toBeVisible();
  await expect(d.getByText('Choose at least one thing it can do')).toBeVisible();

  await d.getByLabel('Name').fill('CI-Deploy');
  await d.getByLabel('See the organization').check();
  await d.getByRole('button', { name: 'Create key' }).click();
  await expect(d.getByText('Another live key has this name')).toBeVisible();
  await expect(created(page)).toHaveCount(0);
});

test('revoking asks first, then the key is gone at once', async ({ page }) => {
  await page.goto('/o/acme/api-keys');
  await keyEntry(page, 'ci-deploy').getByRole('button', { name: 'Revoke ci-deploy…' }).click();
  const confirm = page.getByRole('dialog', { name: 'Revoke ci-deploy?' });
  await expect(confirm.getByText('Anything using it stops working at once.')).toBeVisible();
  await expect(confirm.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await confirm.getByRole('button', { name: 'Revoke key' }).click();

  await expectToast(page, 'Revoked ci-deploy');
  await expect(page.getByRole('heading', { name: '2 active keys' })).toBeVisible();
  if (await page.getByRole('table', { name: 'API keys of Acme' }).isVisible()) {
    await expect(keyEntry(page, 'ci-deploy').getByText(/Revoked .* by Local user/)).toBeVisible();
  } else {
    await expect(page.getByText('2 revoked keys are hidden.', { exact: false })).toBeVisible();
  }
});

test('a list that fails to load keeps the page and offers to try again', async ({ page, request }) => {
  // Every try fails (times 0) until the faults are cleared: the app retries a 500 twice first.
  await request.post('/__mock/fail', { data: { path: '/api-keys', method: 'GET', status: 500, times: 0 } });
  await page.goto('/o/acme/api-keys');
  await expect(heading(page)).toBeVisible();
  await expect(newKey(page)).toBeVisible();
  await expect(page.getByText('Couldn’t load API keys. Check your connection and try again.')).toBeVisible({
    timeout: 15_000,
  });
  await request.delete('/__mock/fail');
  await page.getByRole('button', { name: 'Try again' }).click();
  await expect(page.getByRole('heading', { name: '3 active keys' })).toBeVisible();
});

test('an organization without keys says so', async ({ page }) => {
  await page.goto('/o/example-labs/api-keys');
  await expect(page.getByRole('heading', { name: 'No keys yet' })).toBeVisible();
  await expect(page.getByText('Create one for CI or a script.')).toBeVisible();
  await expectAccessible(page);
});

test('creating a key with an older sign-in confirms it’s you, then creates it', async ({ page }) => {
  const res = await page.request.post('/__mock/reset', { data: { auth: { mode: 'session', signed_in: 'stale' } } });
  expect(res.ok()).toBe(true);
  await page.goto('/o/acme/api-keys');
  await newKey(page).click();
  const d = dialog(page);
  await d.getByLabel('Name').fill('nightly-build');
  await d.getByText('Read only').click();
  await d.getByRole('button', { name: 'Create key' }).click();

  const reauth = page.getByRole('dialog', { name: 'Confirm it’s you' });
  await reauth.getByRole('button', { name: 'Email me a code' }).click();
  await reauth.getByLabel('Code').fill('000000');
  await expect(reauth).toBeHidden();
  await expect(created(page)).toBeVisible();
});
