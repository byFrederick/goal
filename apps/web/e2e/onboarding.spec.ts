// Onboarding step 1 (docs/design/screens/get-started/org.jpg) against the mock: a signed-in person with
// no organization is sent to /get-started, names one, and reaches step 2; an address already taken is
// refused on the address field; an unknown step is not found. Not run in the box: Playwright needs a
// browser, which is not installed here. CI runs it against the mock.
import { type APIRequestContext, expect, type Page, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll } from './support';

/** Signs in a person and resets the mock; with `orgs: 'none'` the person is in no organization yet. */
async function signIn(request: APIRequestContext, options: { orgs?: 'none' } = {}) {
  const res = await request.post('/__mock/reset', {
    data: {
      ...options,
      auth: { mode: 'session', signed_in: 'fresh', email: 'f.cruz@example.com' },
    },
  });
  expect(res.ok()).toBeTruthy();
}

function nameField(page: Page) {
  return page.getByLabel('Organization name');
}

function addressField(page: Page) {
  return page.getByLabel('Address');
}

test('a person with no organization is sent to step 1, creates one and reaches step 2', async ({ page }) => {
  await signIn(page.request, { orgs: 'none' });
  await page.goto('/');

  await expect(page).toHaveURL(/\/get-started$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Name your organization' })).toBeVisible();

  await nameField(page).fill('Goal');
  await expect(addressField(page)).toHaveValue('goal');
  await expect(page.getByRole('button', { name: 'Build software' })).toHaveAttribute('aria-pressed', 'true');
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);

  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page).toHaveURL(/\/o\/goal\/get-started\/code$/);
  await expect(page.getByRole('list', { name: 'Setup, step 2 of 8' })).toBeVisible();
});

test('an address another organization has is refused on the address field', async ({ page }) => {
  // The default seed already has an organization at goal, so the same name is taken.
  await signIn(page.request);
  await page.goto('/get-started');

  await nameField(page).fill('Goal');
  await page.getByRole('button', { name: 'Continue' }).click();

  await expect(page.getByText('goal is taken. Choose another address.')).toBeVisible();
  await expect(addressField(page)).toBeFocused();
  await expect(page).toHaveURL(/\/get-started$/);
});

test('an unknown step under an organization is not found', async ({ page }) => {
  await signIn(page.request);
  await page.goto('/o/goal/get-started/not-a-step');

  await expect(page.getByRole('heading', { name: 'No such page' })).toBeVisible();
});

test('Save and finish later signs out when there is no organization yet', async ({ page }) => {
  await signIn(page.request, { orgs: 'none' });
  await page.goto('/get-started');

  await page.getByRole('button', { name: 'Save and finish later' }).click();
  await expect(page).toHaveURL(/\/sign-in$/);
});

test('step 2 lists the repositories, keeps the choice in the URL and continues to step 3', async ({ page }) => {
  await signIn(page.request);
  await page.goto('/o/goal/get-started/code');

  await expect(page.getByRole('heading', { level: 1, name: 'Connect your code' })).toBeVisible();
  await expect(page.getByText('Connected as acme')).toBeVisible();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);

  await page.locator('label', { hasText: 'acme/goal' }).first().getByRole('checkbox').check();
  await page.getByRole('button', { name: 'Continue with 1 repository' }).click();

  await expect(page).toHaveURL(/\/o\/goal\/get-started\/factory\?/);
});

test('step 2 says GitLab is not available yet', async ({ page }) => {
  await signIn(page.request);
  await page.goto('/o/goal/get-started/code');

  await expect(page.getByRole('button', { name: 'GitLab is coming.' })).toHaveAttribute('aria-disabled', 'true');
});
