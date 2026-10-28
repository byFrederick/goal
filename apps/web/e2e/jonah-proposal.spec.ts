// Proposal 7's page (jonah/proposal.jpg): it shows the picture's files and runs, Approve publishes it, Edit opens the
// draft and Reject records a reason. Not run in the box: Playwright needs a browser, which the box does not have.
import { expect, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll } from './support';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

test('the page shows proposal 7 as pictured: the estimate, the two diffs, the four runs and the details', async ({
  page,
}) => {
  await page.goto('/o/acme/f/software/jonah/proposals/7');
  await expect(page.getByRole('heading', { level: 1, name: /Ask frontend-engineer for a screenshot/ })).toBeVisible();
  await expect(page.getByText('Waiting for your review')).toBeVisible();
  await expect(page.getByText('2 files, v21 to v22')).toBeVisible();
  await expect(page.getByText('Why: 4 runs sent back')).toBeVisible();
  await expect(page.getByText('v22; runs in progress keep v21')).toBeVisible();

  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('Approve publishes proposal 7 and opens the definition at its versions', async ({ page }) => {
  await page.goto('/o/acme/f/software/jonah/proposals/7');
  await page.getByRole('button', { name: 'Approve and publish v22' }).click();
  await expect(page).toHaveURL(/\/definition\/versions/);
});

test('Approve shows the draft-open message on the flagged proposal 8', async ({ page }) => {
  await page.goto('/o/acme/f/software/jonah/proposals/8');
  await page.getByRole('button', { name: 'Approve and publish v22' }).click();
  await expect(page.getByText(/The definition already has an open draft/)).toBeVisible();
});

test('Reject needs a reason, then records it and shows the rejected state', async ({ page }) => {
  await page.goto('/o/acme/f/software/jonah/proposals/7');
  await page.getByRole('button', { name: 'Reject with a reason' }).click();
  const dialog = page.getByRole('dialog', { name: 'Reject with a reason' });
  const submit = dialog.getByRole('button', { name: 'Reject proposal' });
  await expect(submit).toBeDisabled();

  await dialog.getByRole('textbox', { name: 'Reason' }).fill('Put the screenshot rule in the pull request template.');
  await expect(submit).toBeEnabled();
  await submit.click();

  await expect(page.getByText('Rejected')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Approve and publish v22' })).toHaveCount(0);
});

test('the page fits 375 and 768 px and renders in dark mode', async ({ page }) => {
  for (const width of [375, 768]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/o/acme/f/software/jonah/proposals/7');
    await expect(page.getByText('Waiting for your review')).toBeVisible();
    await expectNoHorizontalScroll(page);
  }

  await page.setViewportSize({ width: 1440, height: 900 });
  await page.emulateMedia({ colorScheme: 'dark' });
  await page.goto('/o/acme/f/software/jonah/proposals/7');
  await expect(page.getByText('Waiting for your review')).toBeVisible();
  await expectAccessible(page);
});
