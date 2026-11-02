import { expect, test } from '@playwright/test';
import { expectNoHorizontalScroll, orgNav } from './support';

// Needs you (/o/acme/needs-you): the list, the split detail and the phone's approve view, against the
// mock API. The mock seeds decisions and alerts in the software, sre and support factories.
test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

test('the page lists what waits on you, with the count in the top bar', async ({ page }) => {
  await page.goto('/o/acme/needs-you');
  await expect(page.getByRole('heading', { name: 'Needs you' })).toBeVisible();
  await expect(page.getByRole('list', { name: /Waiting on you/ })).toBeVisible();
  await expectNoHorizontalScroll(page);
});

test('the Decisions tab writes its type to the URL and hides the alerts', async ({ page }) => {
  await page.goto('/o/acme/needs-you');
  await page.getByRole('radio', { name: /^Decisions/ }).check({ force: true });
  await expect(page).toHaveURL(/type=decision/);
  await expect(page.getByRole('radio', { name: /^Decisions/ })).toBeChecked();
});

test('Approvals lists the waiting tool call, and Approve with Always allow removes it', async ({ page }) => {
  // The mock seeds the architect's add_issue_comment call on software#151 (mock/fixtures.ts).
  await page.goto('/o/acme/needs-you');
  await page.getByRole('radio', { name: /^Approvals/ }).check({ force: true });
  await expect(page).toHaveURL(/type=approval/);

  const list = page.getByRole('list', { name: /Waiting on you/ });
  await expect(list.getByRole('link', { name: /architect wants to call add_issue_comment/ })).toBeVisible();

  await page.getByRole('checkbox', { name: 'Always allow architect to add_issue_comment' }).check();
  await page.getByRole('button', { name: /^Approve/ }).click();

  await expect(list.getByRole('link', { name: /architect wants to call/ })).toHaveCount(0);
});

test('the Questions filter shows the agent question and answering it removes the row', async ({ page }) => {
  // Item 14 of the goal factory holds the pictured question (see question.spec.ts); the caller decides there.
  await page.goto('/o/acme/needs-you');
  await page.getByRole('radio', { name: /^Questions/ }).check({ force: true });
  await expect(page).toHaveURL(/type=question/);

  const list = page.getByRole('list', { name: /Waiting on you/ });
  await expect(list.getByRole('link', { name: /frontend-engineer: / })).toBeVisible();

  const card = page.getByRole('region', { name: 'frontend-engineer asks you' });
  await card.getByRole('button', { name: 'Only this factory' }).click();

  await expect(list.getByRole('link', { name: /frontend-engineer: / })).toHaveCount(0);
});

test('J and K move the selection, and the URL names the selected entry', async ({ page }) => {
  await page.goto('/o/acme/needs-you');
  await page.keyboard.press('j');
  await expect(page).toHaveURL(/entry=/);
  await page.keyboard.press('k');
  await expect(page.locator('a[aria-current="page"]')).toHaveCount(1);
});

test.describe('the phone approve view at 375 px', () => {
  test.use({ viewport: { width: 375, height: 812 } });

  test('an approval shows its Always allow checkbox, and Approve with it removes the row', async ({ page }) => {
    await page.goto('/o/acme/needs-you?type=approval');
    await page
      .getByRole('list', { name: /Waiting on you/ })
      .getByRole('link')
      .first()
      .click();
    await page.getByRole('checkbox', { name: 'Always allow architect to add_issue_comment' }).check();
    await page.getByRole('button', { name: /^Approve/ }).click();
    await expect(page.getByRole('list', { name: /Waiting on you/ })).toHaveCount(0);
    await expectNoHorizontalScroll(page);
  });

  test('an entry in the URL shows the approve view with Deny and Approve', async ({ page }) => {
    await page.goto('/o/acme/needs-you?type=decision');
    await page
      .getByRole('list', { name: /Waiting on you/ })
      .getByRole('link')
      .first()
      .click();
    await expect(page.getByRole('link', { name: 'Back to Needs you' })).toBeVisible();
    await expect(page.getByRole('button', { name: /^Approve/ })).toBeVisible();
    await expect(page.getByRole('button', { name: /^Deny/ })).toBeVisible();
    await expectNoHorizontalScroll(page);
  });
});

test('the sidebar Needs you link shows the page', async ({ page }) => {
  await page.goto('/o/acme');
  await orgNav(page)
    .getByRole('link', { name: /^Needs you/ })
    .click();
  await expect(page).toHaveURL(/\/o\/acme\/needs-you$/);
});

test('nothing scrolls sideways at 768 px, where the list narrows beside the detail', async ({ page }) => {
  await page.setViewportSize({ width: 768, height: 900 });
  await page.goto('/o/acme/needs-you');
  await expect(page.getByRole('list', { name: /Waiting on you/ })).toBeVisible();
  const width = await page.evaluate(() => document.documentElement.scrollWidth);
  expect(width).toBeLessThanOrEqual(768);
  await expectNoHorizontalScroll(page);
});

// The pictures for the pull request: this screen at 1440 and 390 px, in both themes. They are attached
// to the report, not compared; the design exports are the reference.
for (const theme of ['light', 'dark'] as const) {
  for (const width of [1440, 390]) {
    test(`needs you screenshot, ${width} px, ${theme}`, async ({ page }, testInfo) => {
      await page.setViewportSize({ width, height: width === 1440 ? 900 : 844 });
      await page.addInitScript((value) => window.localStorage.setItem('goal-theme', value), theme);
      await page.goto('/o/acme/needs-you');
      await expect(page.getByRole('list', { name: /Waiting on you/ })).toBeVisible();
      await expectNoHorizontalScroll(page);
      await testInfo.attach(`needs-you-${width}-${theme}`, {
        body: await page.screenshot({ fullPage: true }),
        contentType: 'image/png',
      });
    });
  }
}
