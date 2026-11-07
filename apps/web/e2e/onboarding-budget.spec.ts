// Onboarding step 5, the Budget step, at 375 and 1440 px: the week is the budget, the day is its share
// (week ÷ 7), the per-run cap is named for what it is, and the exhaustion choice says what the engine does.
// Not run in the box (no browser); CI runs it.
import { expect, test } from '@playwright/test';
import { expectNoHorizontalScroll } from './support';

const widths = [375, 1440];

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

for (const width of widths) {
  test(`the Budget step sets the week and explains the stop at ${width} px`, async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'desktop-1440', 'one project runs both widths');
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/o/goal/get-started/factory');
    await page.getByRole('button', { name: /^Use / }).click();
    await expect(page).toHaveURL(/\/o\/goal\/get-started\/code\?.*factory=/);
    const factory = new URL(page.url()).searchParams.get('factory');
    await page.goto(`/o/goal/get-started/budget?factory=${factory}`);

    await expect(page.getByRole('heading', { level: 1, name: 'Set budgets and targets' })).toBeVisible();
    await expect(
      page.getByText('Goal stops starting new work when the last 7 days’ spend reaches the budget.'),
    ).toBeVisible();
    await expect(page.getByRole('slider', { name: 'A day' })).toHaveCount(0);

    const week = page.getByRole('slider', { name: 'A week' });
    await expect(week).toHaveValue('800');
    await expect(page.getByText('about $114 a day', { exact: true })).toBeVisible();
    await week.fill('1400');
    await expect(page.getByText('about $200 a day', { exact: true })).toBeVisible();

    await expect(page.getByRole('slider', { name: 'Most per run' })).toBeVisible();
    await expect(page.getByRole('slider', { name: 'Most per item' })).toHaveCount(0);

    await expect(
      page.getByText('Running items finish; nothing new starts until the last 7 days’ spend is under the budget.'),
    ).toBeVisible();
    const tip = page.getByRole('button', { name: 'About when a budget runs out' });
    if (width === 375) expect((await tip.boundingBox())?.height ?? 0).toBeGreaterThanOrEqual(44);
    await tip.click();
    await expect(page.locator('[data-radix-popper-content-wrapper]')).toContainText('no reset at midnight');

    await page.getByRole('button', { name: 'Continue' }).click();
    await expect(page).toHaveURL(/\/get-started\/budget/);
    await expectNoHorizontalScroll(page);
  });
}

for (const width of widths) {
  test(`Back from Intake to Budget and Line works on the published factory at ${width} px`, async ({
    page,
    request,
  }, testInfo) => {
    test.skip(testInfo.project.name !== 'desktop-1440', 'one project runs both widths');
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/o/goal/get-started/factory');
    await page.getByRole('button', { name: /^Use / }).click();
    await expect(page).toHaveURL(/\/o\/goal\/get-started\/code\?.*factory=/);
    const factory = new URL(page.url()).searchParams.get('factory');
    // Line holds Continue until the factory has a model key, so save one first.
    await page.goto(`/o/goal/get-started/line?factory=${factory}`);
    await page
      .locator('input')
      .and(page.getByLabel('Model key', { exact: true }))
      .fill('test-token');
    await page.getByRole('button', { name: 'Save the key' }).click();
    await expect(page.getByText('Saved as the secret model-key, open to this factory.')).toBeVisible();
    await page.getByRole('button', { name: 'Continue' }).click();
    await expect(page).toHaveURL(/\/get-started\/budget/);
    await expect(page.getByRole('slider', { name: 'A week' })).toHaveValue('800');
    await page.getByRole('button', { name: 'Continue' }).click();
    await expect(page).toHaveURL(/\/get-started\/intake/);

    // Back to Budget shows the running version; the first change opens a draft, Continue publishes it.
    await page.getByRole('link', { name: 'Back' }).click();
    await expect(page).toHaveURL(/\/get-started\/budget/);
    const week = page.getByRole('slider', { name: 'A week' });
    await expect(week).toHaveValue('800');
    await expect(page.getByText(/Choose a template again/)).toHaveCount(0);
    await week.fill('1400');
    await expect(page.getByText('about $200 a day', { exact: true })).toBeVisible();
    await page.getByRole('button', { name: 'Continue' }).click();
    await expect(page).toHaveURL(/\/get-started\/intake/);

    const model = await (await request.get(`/v1/orgs/goal/factories/${factory}/versions/current/model`)).json();
    expect(model.model.review.budget.weekly_usd).toBe(1400);

    // Back twice reaches Line; no message appears.
    await page.getByRole('link', { name: 'Back' }).click();
    await expect(page).toHaveURL(/\/get-started\/budget/);
    await expect(week).toHaveValue('1400');
    await page.getByRole('link', { name: 'Back' }).click();
    await expect(page).toHaveURL(/\/get-started\/line/);
    await expect(page.getByRole('heading', { level: 1, name: 'Shape the line' })).toBeVisible();
    await expect(page.getByText(/Choose a template again|has no draft/)).toHaveCount(0);
    await page.getByRole('button', { name: 'Continue' }).click();
    await expect(page).toHaveURL(/\/get-started\/budget/);
    await expectNoHorizontalScroll(page);
  });
}
