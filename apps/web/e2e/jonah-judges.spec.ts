// Jonah's judges (build the Jonah judges and judge screens, jonah/judges.jpg and jonah/judge.jpg): the four
// judges of the Software factory, a judge's switch, and a judge's page with its rubric, outcomes and sliders,
// at 375, 768 and 1440 px and in dark mode. Not run in the box: Playwright needs a browser, which the box
// does not have.
import { expect, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll } from './support';

const widths = [375, 768, 1440] as const;

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

test('the judges page lists the four judges with their 30 days and the cost of the month', async ({ page }) => {
  await page.goto('/o/acme/f/software/jonah/judges');
  await expect(page.getByRole('heading', { level: 1, name: 'Jonah' })).toBeVisible();
  await expect(page.getByText('Judges read a sample of runs and score them against a rubric you write.')).toBeVisible();
  await expect(page.getByText('$11.60 in the last 30 days')).toBeVisible();

  const table = page.getByRole('table');
  await expect(table.getByRole('row')).toHaveCount(5);
  await expect(table.getByRole('link', { name: 'Review reason actionable' })).toBeVisible();
  await expect(table.getByText('81%', { exact: true })).toBeVisible();
  await expect(page.getByRole('switch', { name: 'Efficiency on' })).toHaveAttribute('aria-checked', 'true');
  await expect(page.getByRole('button', { name: 'New judge' })).toBeVisible();

  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('a judge is read on its own page: the summary, the rubric file, the outcomes and the sliders', async ({
  page,
}) => {
  await page.goto('/o/acme/f/software/jonah/judges/review-reason-actionable');
  await expect(page.getByRole('heading', { level: 1, name: 'Review reason actionable' })).toBeVisible();
  await expect(page.getByText('Scored 46 runs in 30 days, 81% passed, $3.20')).toBeVisible();
  await expect(page.getByText('evals/judges/review-reason-actionable.md')).toBeVisible();
  await expect(page.getByLabel('Outcome 1 label')).toHaveValue('Actionable');
  await expect(page.getByRole('slider', { name: 'Pass mark' })).toHaveValue('0.7');
  await expect(page.getByRole('slider', { name: 'Sample' })).toHaveValue('25');
  await expect(page.getByRole('button', { name: 'Try on 10 recent runs' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Save changes' })).toBeVisible();

  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('saving an edited judge keeps the edit on the page', async ({ page }) => {
  await page.goto('/o/acme/f/software/jonah/judges/review-reason-actionable');
  await page.getByRole('textbox', { name: 'What it checks' }).fill('A sent-back reason names the fix');
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.getByRole('textbox', { name: 'What it checks' })).toHaveValue('A sent-back reason names the fix');
});

test('the switch on the list publishes the judge with its flag flipped', async ({ page }) => {
  await page.goto('/o/acme/f/software/jonah/judges');
  const toggle = page.getByRole('switch', { name: 'Plain language on' });
  await toggle.click();
  await expect(toggle).toHaveAttribute('aria-checked', 'false');
});

test('a factory with no judges says so and offers a new one', async ({ page }) => {
  await page.goto('/o/acme/f/goal/jonah/judges');
  await expect(page.getByText('No judges yet')).toBeVisible();
  await expect(page.getByRole('button', { name: 'New judge' })).toBeVisible();
});

test('the Judges tab is current on the list and the Bake-offs tab says it is not available yet', async ({ page }) => {
  await page.goto('/o/acme/f/software/jonah/judges');
  await expect(page.getByRole('navigation', { name: 'Jonah' }).getByRole('link', { name: 'Judges' })).toHaveAttribute(
    'aria-current',
    'page',
  );
  await page.getByRole('button', { name: 'Bake-offs' }).hover();
  await expect(page.getByText('Not available yet').first()).toBeVisible();
});

for (const width of widths) {
  test(`both judges screens fit ${width} px with no sideways scroll`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/o/acme/f/software/jonah/judges');
    await expect(page.getByRole('table')).toBeVisible();
    await expectNoHorizontalScroll(page);

    await page.goto('/o/acme/f/software/jonah/judges/review-reason-actionable');
    await expect(page.getByRole('heading', { level: 1, name: 'Review reason actionable' })).toBeVisible();
    await expectNoHorizontalScroll(page);
  });
}

test('both judges screens render in dark mode', async ({ page }) => {
  await page.emulateMedia({ colorScheme: 'dark' });
  await page.goto('/o/acme/f/software/jonah/judges');
  await expect(page.getByRole('table')).toBeVisible();
  await expectAccessible(page);
  await page.goto('/o/acme/f/software/jonah/judges/review-reason-actionable');
  await expect(page.getByRole('heading', { level: 1, name: 'Review reason actionable' })).toBeVisible();
  await expectAccessible(page);
});
