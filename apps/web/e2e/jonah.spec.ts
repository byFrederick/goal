// Jonah's overview and its Proposals tab (build the Jonah overview, jonah/jonah.jpg): the proposals waiting, the
// policy, what Jonah noticed, the review that Look for improvements now queues, and the Proposals tab's state filter.
// Not run in the box: Playwright needs a browser, which the box does not have.
import { expect, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll } from './support';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

test('the overview shows the two proposals waiting, the policy and what Jonah noticed', async ({ page }) => {
  await page.goto('/o/acme/f/software/jonah');
  await expect(page.getByRole('heading', { level: 1, name: 'Jonah' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Waiting for your review' })).toBeVisible();

  await expect(page.getByText('Proposal 7')).toBeVisible();
  await expect(page.getByText('Proposal 8')).toBeVisible();
  await expect(page.getByText('Based on 4 runs sent back')).toBeVisible();
  await expect(page.getByText('$6.40 this month')).toBeVisible();
  await expect(page.getByText('Watching, 3 of 5')).toBeVisible();
  await expect(page.getByRole('link', { name: 'All proposals' })).toHaveAttribute(
    'href',
    '/o/acme/f/software/jonah?tab=proposals',
  );

  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('the controls not built yet are disabled, and the pass rates are not shown', async ({ page }) => {
  await page.goto('/o/acme/f/software/jonah');
  await expect(page.getByRole('button', { name: 'Review' }).first()).toHaveAttribute('aria-disabled', 'true');
  await expect(page.getByText('Pass rate by judge, last 30 days')).toBeVisible();
  await expect(page.getByText('Not available yet').first()).toBeVisible();
});

test('Look for improvements now queues a review, and a second one says one is running', async ({ page }) => {
  await page.goto('/o/acme/f/software/jonah');
  await page.getByRole('button', { name: 'Look for improvements now' }).click();
  await expect(page.getByText('Review queued.')).toBeVisible();

  await page.getByRole('button', { name: 'Look for improvements now' }).click();
  await expect(page.getByText('A review is already running.')).toBeVisible();
});

test('the Proposals tab filters by the state in the URL', async ({ page }) => {
  await page.goto('/o/acme/f/software/jonah?tab=proposals');
  await expect(
    page.getByRole('navigation', { name: 'Jonah' }).getByRole('link', { name: 'Proposals' }),
  ).toHaveAttribute('aria-current', 'page');
  await page.getByRole('radio', { name: 'Applied' }).check();
  await expect(page).toHaveURL(/state=applied/);
  await expect(page.getByText('Name the agent in every sent-back note')).toBeVisible();
});

test('the overview fits 375 and 768 px and renders in dark mode', async ({ page }) => {
  for (const width of [375, 768]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/o/acme/f/software/jonah');
    await expect(page.getByRole('heading', { name: 'Waiting for your review' })).toBeVisible();
    await expectNoHorizontalScroll(page);
  }

  await page.setViewportSize({ width: 1440, height: 900 });
  await page.emulateMedia({ colorScheme: 'dark' });
  await page.goto('/o/acme/f/software/jonah');
  await expect(page.getByRole('heading', { name: 'Waiting for your review' })).toBeVisible();
  await expectAccessible(page);
});

test('a factory with no version says the review needs a publish first', async ({ page }) => {
  await page.goto('/o/acme/f/goal/jonah');
  await page.getByRole('button', { name: 'Look for improvements now' }).click();
  await expect(page.getByText('Publish a version first.')).toBeVisible();
});
