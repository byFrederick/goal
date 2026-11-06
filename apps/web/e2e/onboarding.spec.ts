// Onboarding step 1 (docs/design/screens/get-started/org.jpg) against the mock: a signed-in person with
// no organization is sent to /get-started, names one, and reaches step 2 (Factory); an address already taken is
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
  await expect(page).toHaveURL(/\/o\/goal\/get-started\/factory$/);
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
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.goto('/o/goal/get-started/not-a-step');

  await expect(page.getByRole('heading', { name: 'No such page' })).toBeVisible();
  expect(errors).toEqual([]);
});

test('Save and finish later signs out when there is no organization yet', async ({ page }) => {
  await signIn(page.request, { orgs: 'none' });
  await page.goto('/get-started');

  await page.getByRole('button', { name: 'Save and finish later' }).click();
  await expect(page).toHaveURL(/\/sign-in$/);
});

/** Step 2: the first template is chosen and the factory made; the Code step opens. */
async function makeFactory(page: Page) {
  await page.goto('/o/goal/get-started/factory');
  await page.getByRole('button', { name: /^Use / }).click();
  await expect(page).toHaveURL(/\/o\/goal\/get-started\/code\?.*factory=/);
}

/** Step 3: Read and write on one repository, the token tested by the mock, then Continue. */
async function connectCode(page: Page, url = 'https://github.com/acme/goal') {
  await page.getByRole('radio', { name: /^Read and write/ }).check();
  await page.getByLabel('Repository', { exact: true }).fill(url);
  await page.getByLabel('Token').fill('test-token');
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page.getByText('acme/goal: Can read and push')).toBeVisible();
  await page.getByRole('button', { name: 'Continue' }).click();
}

test('step 2 creates the factory from the first template and opens the Code step', async ({ page }) => {
  await signIn(page.request);
  await page.goto('/o/goal/get-started/factory');

  // The templates are listed by title, and the first is chosen.
  await expect(page.getByRole('heading', { level: 1, name: 'Start your first factory' })).toBeVisible();
  await expect(page.getByRole('radio', { name: /Start empty/ })).toBeVisible();
  await page.getByRole('button', { name: /^Use / }).click();
  await expect(page).toHaveURL(/\/o\/goal\/get-started\/code\?.*factory=/);
});

test('step 3 takes a URL and a token, tests them on the factory, keeps the secret name in the address and continues', async ({
  page,
}) => {
  await signIn(page.request);
  await makeFactory(page);

  await expect(page.getByRole('heading', { level: 1, name: 'Connect your code' })).toBeVisible();
  await page.getByRole('radio', { name: /^Read and write/ }).check();
  await expect(page.getByRole('button', { name: 'Continue' })).toBeDisabled();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);

  await connectCode(page);

  await expect(page).toHaveURL(/\/o\/goal\/get-started\/line\?/);
  await expect(page).toHaveURL(/secret=github-acme/);
  expect(page.url()).not.toContain('test-token');
});

test('step 3 shows the host’s reason when the token cannot push, and keeps Continue disabled', async ({ page }) => {
  await signIn(page.request);
  await makeFactory(page);
  await page.getByRole('radio', { name: /^Read and write/ }).check();
  await page.getByLabel('Repository', { exact: true }).fill('https://github.com/acme/goal-readonly');
  await page.getByLabel('Token').fill('test-token');
  await page.getByRole('button', { name: 'Continue' }).click();

  await expect(page.getByText(/Can read but not push\. The token has no write access/)).toBeVisible();
  await expect(page.getByRole('button', { name: 'Continue' })).toBeDisabled();
});

test('step 3 continues without code when the template allows No code', async ({ page }) => {
  await signIn(page.request);
  await makeFactory(page);
  const none = page.getByRole('radio', { name: /^No code/ });
  test.skip(await none.isDisabled(), 'the first template needs code');
  await none.check();
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page).toHaveURL(/\/o\/goal\/get-started\/line\?.*factory=/);
});

test('step 4 shapes the line of the factory made in step 2', async ({ page }) => {
  await signIn(page.request);
  await makeFactory(page);
  await connectCode(page);

  await expect(page).toHaveURL(/\/o\/goal\/get-started\/line\?.*factory=/);
  await expect(page.getByRole('heading', { level: 1, name: 'Shape the line' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Not available yet' })).toHaveAttribute('aria-disabled', 'true');
  await expect(page.getByRole('button', { name: 'Continue' })).toBeEnabled();
  await expect(page.getByRole('button', { name: 'Add a station here' }).first()).toBeVisible();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('step 4 without a factory links back to the step that makes one', async ({ page }) => {
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
  await makeFactory(page);
  await connectCode(page);
  await expect(page).toHaveURL(/\/o\/goal\/get-started\/line\?.*factory=/);

  await page.getByRole('button', { name: 'Continue' }).click();
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

  // The checklist is for a new factory: the starter item finished, so the Floor shows no checklist and no
  // "How work gets in" row, and the line keeps the full width (frontend.md, "New factory row"; #441).
  await expect(page.getByRole('heading', { level: 1, name: 'Floor' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Finish setting up' })).toHaveCount(0);
  await expect(page.getByRole('img', { name: /of 9 done/ })).toHaveCount(0);
});

for (const size of [
  { width: 1440, height: 1000 },
  { width: 390, height: 844 },
]) {
  test(`step 3 keeps focus on Continue after the token test at ${size.width} px`, async ({ page }) => {
    await signIn(page.request);
    await page.setViewportSize(size);
    await makeFactory(page);
    await page.getByRole('radio', { name: /^Read and write/ }).check();
    await page.getByLabel('Repository', { exact: true }).fill('https://github.com/acme/goal-readonly');
    await page.getByLabel('Token').fill('test-token');
    const next = page.getByRole('button', { name: 'Continue' });
    await next.focus();
    await next.press('Enter');

    await expect(page.getByRole('status')).toContainText(/Can read but not push/);
    await expect(next).toBeFocused();
  });
}

for (const width of [375, 768, 1440]) {
  test(`step 3's preview keeps the picked template's line at ${width} px`, async ({ page }) => {
    await signIn(page.request);
    await page.setViewportSize({ width, height: 900 });
    await makeFactory(page);

    const preview = page.getByRole('complementary', { name: 'Preview of your factory' });
    await expect(preview.getByRole('region', { name: 'Your factory' })).toBeVisible();
    await expect(preview.getByRole('list', { name: 'The line' }).getByRole('listitem').first()).toBeVisible();
    await expect(preview.getByText('The line appears when you pick a template')).toHaveCount(0);
  });
}

test('Back from the Line step keeps Read and write on the Code step, also after a reload', async ({ page }) => {
  await signIn(page.request);
  await makeFactory(page);
  await connectCode(page);
  await expect(page).toHaveURL(/\/o\/goal\/get-started\/line\?/);

  await page.getByRole('link', { name: 'Back' }).click();
  await expect(page).toHaveURL(/\/o\/goal\/get-started\/code\?.*access=write/);
  await expect(page.getByRole('radio', { name: /^Read and write/ })).toBeChecked();
  await expect(page.getByLabel('Repository', { exact: true })).toHaveValue('https://github.com/acme/goal');

  await page.reload();
  await expect(page.getByRole('radio', { name: /^Read and write/ })).toBeChecked();
});
