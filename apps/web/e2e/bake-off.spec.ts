// Jonah's Bake-off (build the Jonah bake-off screen, jonah/bake-off.jpg): the mock's bake-off of
// frontend-engineer, its banner, plot, grid and results at each width, in dark mode, and the actions. Not run
// in the box: Playwright needs a browser, which the box does not have.
import { expect, type Page, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll } from './support';

const url = '/o/acme/f/software/jonah/bake-offs';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

async function expectPicture(page: Page) {
  await expect(page.getByRole('heading', { level: 1, name: 'Which model for frontend-engineer?' })).toBeVisible();
  await expect(
    page.getByText('3 models on the same 5 frontend cases, 3 tries each. Finished Oct 25 at 18:40, cost $14.20.'),
  ).toBeVisible();
  await expect(
    page.getByRole('heading', { name: 'claude-haiku-5-5: the same pass rate at a tenth of the cost' }),
  ).toBeVisible();
  await expect(page.getByRole('img', { name: /haiku 5\.5 93% at \$0\.24/ })).toBeVisible();
  await expect(page.getByText('Out of 3')).toBeVisible();
  await expect(page.getByRole('table', { name: 'Results of the bake-off, one row per model' })).toBeVisible();
}

test('the Bake-offs tab opens the newest bake-off as pictured', async ({ page }) => {
  await page.goto(url);
  await expect(page).toHaveURL(/\/jonah\/bake-offs\/bake-off-1$/);
  await expectPicture(page);
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

for (const width of [375, 768, 1440]) {
  test(`the bake-off lays out at ${width} px without scrolling sideways`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.goto(`${url}/bake-off-1`);
    await expectPicture(page);
    await expectNoHorizontalScroll(page);
  });
}

test.describe('in dark mode', () => {
  test.use({ colorScheme: 'dark' });

  test('the bake-off reads in the dark theme', async ({ page }) => {
    await page.goto(`${url}/bake-off-1`);
    await expectPicture(page);
    await expectAccessible(page);
  });
});

test('Run again posts a re-run and moves to the new bake-off', async ({ page }) => {
  await page.goto(`${url}/bake-off-1`);
  await page.getByRole('button', { name: 'Run again' }).click();
  await expect(page).not.toHaveURL(/\/bake-off-1$/);
  await expect(page.getByText('Queued')).toBeVisible();
});

test("New bake-off goes to the factory's Agents screen", async ({ page }) => {
  await page.goto(`${url}/bake-off-1`);
  await page.getByRole('link', { name: 'New bake-off' }).click();
  await expect(page).toHaveURL(/\/o\/acme\/f\/software\/agents$/);
});

test('Apply to the definition writes the model and links to the draft', async ({ page }) => {
  await page.goto(`${url}/bake-off-1`);
  await page.getByRole('button', { name: 'Apply to the definition' }).click();
  await expect(page.getByRole('link', { name: 'Open the draft' })).toHaveAttribute(
    'href',
    '/o/acme/f/software/definition',
  );
});
