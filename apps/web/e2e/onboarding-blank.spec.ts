// Start empty, end to end (assembly: Start empty must finish). A new organization picks Start empty and
// No code, goes through Line, Budget (which publishes without a refusal), Intake, People and First item to
// the Floor. The project decides the width: phone-375, tablet-768 and desktop-1440. Not run in the box:
// Playwright needs a browser, which is not installed here. CI runs it against the mock.
import { expect, test } from '@playwright/test';
import { expectNoHorizontalScroll } from './support';

test('a new organization starts empty, without code, and reaches the Floor with no refused publish', async ({
  page,
}) => {
  const reset = await page.request.post('/__mock/reset', {
    data: { orgs: 'none', auth: { mode: 'session', signed_in: 'fresh', email: 'f.cruz@example.com' } },
  });
  expect(reset.ok()).toBeTruthy();
  await page.goto('/');

  // Step 1: the organization.
  await expect(page).toHaveURL(/\/get-started$/);
  await page.getByLabel('Organization name').fill('Blank');
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page).toHaveURL(/\/o\/blank\/get-started\/factory$/);

  // Step 2: Start empty makes the factory.
  await page.getByRole('radio', { name: /Start empty/ }).check();
  await page.getByRole('button', { name: /^Use / }).click();
  await expect(page).toHaveURL(/\/o\/blank\/get-started\/code\?.*factory=/);

  // Step 3: no code.
  await page.getByRole('radio', { name: /^No code/ }).check();
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page).toHaveURL(/\/o\/blank\/get-started\/line\?.*factory=/);
  await expectNoHorizontalScroll(page);

  // Step 4: the blank line has one Work station and one agent; Continue opens once the model key is set.
  await expect(page.getByRole('heading', { level: 1, name: 'Shape the line' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Continue' })).toBeDisabled();
  await expect(page.getByText('Add the model key, then continue.')).toBeVisible();
  await page
    .locator('input')
    .and(page.getByLabel('Model key', { exact: true }))
    .fill('test-token');
  await page.getByRole('button', { name: 'Save the key' }).click();
  await expect(page.getByText('Saved as the secret model-key, open to this factory.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Continue' })).toBeEnabled();
  await page.getByRole('button', { name: 'Continue' }).click();

  // Step 5: the publish is not refused.
  await expect(page.getByRole('heading', { level: 1, name: 'Set budgets and targets' })).toBeVisible();
  await expectNoHorizontalScroll(page);
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page).toHaveURL(/\/o\/blank\/get-started\/intake\?.*factory=/);
  await expect(page.getByText('The factory has problems, so it is not published')).toHaveCount(0);

  // Steps 6 to 8.
  await expect(page.getByRole('heading', { level: 1, name: 'Choose where work comes from' })).toBeVisible();
  await expectNoHorizontalScroll(page);
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page).toHaveURL(/\/o\/blank\/get-started\/people\?.*factory=/);
  await expectNoHorizontalScroll(page);
  await page.getByRole('link', { name: 'Skip for now' }).click();
  await expect(page).toHaveURL(/\/o\/blank\/get-started\/first-item\?.*factory=/);
  await expect(page.getByRole('heading', { level: 1, name: 'Watch your first item' })).toBeVisible();
  await expectNoHorizontalScroll(page);

  await page.getByRole('link', { name: 'Go to the Floor' }).click();
  await expect(page).toHaveURL(/\/o\/blank\/f\//);
  await expect(page.getByRole('heading', { level: 1, name: 'Floor' })).toBeVisible();
});
