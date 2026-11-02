// Jonah's overview and its Proposals tab (build the Jonah overview, jonah/jonah.jpg): the proposals waiting, the
// policy, what Jonah noticed, the disabled Look for improvements now button, and the Proposals tab's state filter.
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

test('each proposal links to its page, the Evals control is not available, and the pass rates are not shown', async ({
  page,
}) => {
  await page.goto('/o/acme/f/software/jonah');
  await expect(page.getByRole('link', { name: 'Review' }).first()).toHaveAttribute('href', /\/jonah\/proposals\/8$/);
  await expect(page.getByRole('button', { name: 'Evals' }).first()).toHaveAttribute('aria-disabled', 'true');
  await expect(page.getByText('Pass rate by judge, last 30 days')).toBeVisible();
  await expect(page.getByText('Not available yet').first()).toBeVisible();
});

test("Look for improvements now is not available, with no sentence beside it, until Jonah's review loop ships", async ({
  page,
}) => {
  await page.goto('/o/acme/f/software/jonah');
  const look = page.getByRole('button', { name: 'Look for improvements now' });
  await expect(look).toHaveAttribute('aria-disabled', 'true');
  await expect(page.getByText("Reviews start when Jonah's review loop ships")).toHaveCount(0);
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

test('a factory with no version also shows the disabled review button', async ({ page }) => {
  await page.goto('/o/acme/f/goal/jonah');
  await expect(page.getByRole('button', { name: 'Look for improvements now' })).toHaveAttribute(
    'aria-disabled',
    'true',
  );
});

// Every Jonah page at 1440 and 390 px in both themes: a screenshot of each, and nothing wider than the screen.
// Not run in the box: Playwright needs a browser, which the box does not have.
const jonahPages = [
  ['overview', '/o/acme/f/software/jonah', 'Overview'],
  ['judges', '/o/acme/f/software/jonah/judges', 'Judges'],
  ['evals', '/o/acme/f/software/jonah/evals', 'Evals'],
  ['bake-offs', '/o/acme/f/software/jonah/bake-offs', 'Bake-offs'],
] as const;

for (const scheme of ['light', 'dark'] as const) {
  for (const width of [1440, 390]) {
    for (const [name, url, tab] of jonahPages) {
      test(`${name} at ${width} px in ${scheme}: current tab, no sideways scroll, screenshot`, async ({ page }) => {
        await page.setViewportSize({ width, height: 900 });
        await page.emulateMedia({ colorScheme: scheme, reducedMotion: 'reduce' });
        await page.goto(url);
        const tabs = page.getByRole('navigation', { name: 'Jonah' });
        await expect(tabs.locator('[aria-current="page"]')).toHaveCount(1);
        await expect(tabs.getByRole('link', { name: tab })).toHaveAttribute('aria-current', 'page');
        await expectNoHorizontalScroll(page);
        await expect(page).toHaveScreenshot(`jonah-${name}-${width}-${scheme}.png`);
      });
    }
  }
}
