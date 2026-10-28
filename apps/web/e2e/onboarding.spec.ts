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

test('step 3 creates the factory from the first template, and step 4 shapes its line', async ({ page }) => {
  await signIn(page.request);
  await page.goto('/o/goal/get-started/code');
  await page.locator('label', { hasText: 'acme/goal' }).first().getByRole('checkbox').check();
  await page.getByRole('button', { name: 'Continue with 1 repository' }).click();

  // The templates are listed by title, and the first is chosen.
  await expect(page.getByRole('heading', { level: 1, name: 'Start your first factory' })).toBeVisible();
  await expect(page.getByRole('radio', { name: /Start empty/ })).toBeVisible();
  await page.getByRole('button', { name: /^Use / }).click();

  await expect(page).toHaveURL(/\/o\/goal\/get-started\/line\?.*factory=/);
  await expect(page.getByRole('heading', { level: 1, name: 'Shape the line' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Not available yet' })).toHaveAttribute('aria-disabled', 'true');
  await expect(page.getByRole('link', { name: 'Continue' })).toHaveAttribute('href', /\/get-started\/budget\?factory=/);
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('step 4 without a factory links back to step 3', async ({ page }) => {
  await signIn(page.request);
  await page.goto('/o/goal/get-started/line');

  await expect(page.getByText('Choose a template first, then shape its line.')).toBeVisible();
  await page.getByRole('link', { name: 'Back' }).click();
  await expect(page).toHaveURL(/\/o\/goal\/get-started\/factory/);
});

test('step 4 continues to step 5, which publishes the factory, step 6 turns on a trigger, step 7 invites people and step 8 watches the first item', async ({
  page,
}) => {
  await signIn(page.request);
  await page.goto('/o/goal/get-started/code');
  await page.locator('label', { hasText: 'acme/goal' }).first().getByRole('checkbox').check();
  await page.getByRole('button', { name: 'Continue with 1 repository' }).click();
  await page.getByRole('button', { name: /^Use / }).click();
  await expect(page).toHaveURL(/\/o\/goal\/get-started\/line\?.*factory=/);

  await page.getByRole('link', { name: 'Continue' }).click();
  await expect(page).toHaveURL(/\/o\/goal\/get-started\/budget\?.*factory=/);
  await expect(page.getByRole('heading', { level: 1, name: 'Set budgets and targets' })).toBeVisible();
  await expect(page.getByRole('slider', { name: 'A day' })).toHaveValue('150');
  await expect(page.getByText('fits about 48 items')).toBeVisible();
  await expect(page.getByRole('radio', { name: /Finish what's started/ })).toBeChecked();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);

  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page).toHaveURL(/\/o\/goal\/get-started\/intake\?.*factory=/);
  await expect(page.getByRole('heading', { level: 1, name: 'Choose where work comes from' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Starter triggers' })).toBeVisible();
  // The template's GitHub triggers need an endpoint the new factory does not have: they show Connect, not a switch.
  await expect(page.getByRole('link', { name: 'Connect GitHub' }).first()).toBeVisible();
  await expect(page.getByRole('switch', { name: 'issues-labeled-factory' })).toBeDisabled();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);

  await page.getByRole('switch', { name: 'weekly-dependency-audit' }).click();
  await page.getByRole('button', { name: 'Turn on 1 trigger' }).click();
  await expect(page).toHaveURL(/\/o\/goal\/get-started\/people\?.*factory=/);

  // Step 7: a Builder is added and sent; the People card lists the invitation.
  await expect(page.getByRole('heading', { level: 1, name: 'Invite people' })).toBeVisible();
  await page.getByLabel('Email').fill('alex@example.com');
  await page.getByRole('button', { name: 'Add' }).click();
  await expect(page.getByRole('list', { name: 'People to invite' }).getByText('alex@example.com')).toBeVisible();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
  await page.getByRole('button', { name: 'Send 1 invite' }).click();
  await expect(page).toHaveURL(/\/o\/goal\/get-started\/first-item\?.*factory=/);

  // Step 8: the starter item is submitted once and its card shows where it is.
  await expect(page.getByRole('heading', { level: 1, name: 'Watch your first item' })).toBeVisible();
  await expect(page.getByText('Created from the setup')).toBeVisible();
  await expect(page.getByText('Cost so far')).toBeVisible();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
  await page.getByRole('link', { name: 'Go to the Floor' }).click();
  await expect(page).toHaveURL(/\/o\/goal\/f\//);

  // The Floor's checklist: the flow sent an invite and finished the starter item, so seven of nine are done.
  // Slack and knowledge are still to do, and their rows link to the pages that do them.
  await expect(page.getByRole('heading', { name: 'Finish setting up' })).toBeVisible();
  await expect(page.getByRole('img', { name: '7 of 9 done' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Connect' })).toHaveAttribute(
    'href',
    '/o/goal/connections/new?kind=slack',
  );
  await expect(page.getByRole('link', { name: 'Add' })).toHaveAttribute('href', '/o/goal/knowledge/new');
});
