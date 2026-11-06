import { expect, test } from '@playwright/test';
import { expectNoClippedElements, expectNoHorizontalScroll, openNavigation, orgNav } from './support';
import { boxOf } from './support-boxes';

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

test('the Alerts tab opens the failing source and the conversion that waits on the factory', async ({ page }) => {
  await page.goto('/o/acme/needs-you');
  await page.getByRole('radio', { name: /^Alerts/ }).click({ force: true });
  await expect(page).toHaveURL(/type=alert/);
  const list = page.getByRole('list', { name: /Waiting on you/ });

  await expect(list.getByText(/ \d+ times$/).first()).toBeVisible();
  await list.getByRole('link', { name: /dashboards, which .* reads, does not sync/ }).click();
  const detail = page.getByRole('article');
  await expect(detail.getByRole('heading', { name: /^dashboards, which .* reads, does not sync$/ })).toBeVisible();
  await expect(detail.getByRole('link', { name: 'Open dashboards' })).toBeVisible();

  // Below 1024 px the entry replaces the list; Back returns to it.
  const back = page.getByRole('link', { name: 'Back to Needs you' });
  if (await back.isVisible()) await back.click();
  await list.getByRole('link', { name: /could not move to secrets/ }).click();
  await expect(detail.getByRole('heading', { name: /could not move to secrets$/ })).toBeVisible();
  await expect(detail.getByText(/connection aws-prod uses the aws kind/)).toBeVisible();
  await expect(detail.getByText(/connection zendesk-prod has no secret/)).toBeVisible();
  await expect(detail.getByRole('link', { name: 'Open the definition' })).toBeVisible();
  await expect(page.getByText(/expedite/i)).toHaveCount(0);
});

test('the Decisions tab writes its type to the URL and hides the alerts', async ({ page }) => {
  await page.goto('/o/acme/needs-you');
  await page.getByRole('radio', { name: /^Decisions/ }).click({ force: true });
  await expect(page).toHaveURL(/type=decision/);
  await expect(page.getByRole('radio', { name: /^Decisions/ })).toBeChecked();
});

test('Approvals lists the waiting tool call, and Approve with Always allow removes it', async ({ page }) => {
  // The mock seeds the architect's add_issue_comment call on software#151 (mock/fixtures.ts).
  await page.goto('/o/acme/needs-you');
  await page.getByRole('radio', { name: /^Approvals/ }).click({ force: true });
  await expect(page).toHaveURL(/type=approval/);

  const list = page.getByRole('list', { name: /Waiting on you/ });
  const waiting = list.getByRole('link', { name: /architect wants to call add_issue_comment/ });
  await expect(waiting).toBeVisible();
  await waiting.click(); // below 1024 px an entry opens as its own view

  await page.getByRole('checkbox', { name: 'Always allow architect to add_issue_comment' }).check();
  await page.getByRole('button', { name: /^Approve/ }).click();

  await expect(list.getByRole('link', { name: /architect wants to call/ })).toHaveCount(0);
});

test('the Questions filter shows the agent question and answering it removes the row', async ({ page }) => {
  // Item 14 of the goal factory holds the pictured question (see question.spec.ts); the caller decides there.
  await page.goto('/o/acme/needs-you');
  await page.getByRole('radio', { name: /^Questions/ }).click({ force: true });
  await expect(page).toHaveURL(/type=question/);

  const list = page.getByRole('list', { name: /Waiting on you/ });
  await expect(list.getByRole('link', { name: /frontend-engineer: / })).toBeVisible();
  await list.getByRole('link', { name: /frontend-engineer: / }).click(); // below 1024 px an entry opens as its own view

  const card = page.getByRole('region', { name: 'frontend-engineer asks you' });
  await card.getByRole('button', { name: 'Only this factory' }).click();

  await expect(list.getByRole('link', { name: /frontend-engineer: / })).toHaveCount(0);
});

test('J and K move the selection, and the URL names the selected entry', async ({ page }) => {
  // Below 1024 px the list stands alone and an entry opens as its own view, so there is no selection to move.
  test.skip((page.viewportSize()?.width ?? 0) < 1024, 'the split with a selection starts at 1024 px');
  await page.goto('/o/acme/needs-you');
  await expect(page.getByRole('list', { name: /Waiting on you/ })).toBeVisible();
  await page.keyboard.press('j');
  await expect(page).toHaveURL(/entry=/);
  await page.keyboard.press('k');
  await expect(page.getByRole('list', { name: /Waiting on you/ }).locator('a[aria-current="page"]')).toHaveCount(1);
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

test.describe('Needs you at 390 px', () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test('shows the filter on one row and only the list, and Back returns to the same scroll position', async ({
    page,
  }) => {
    await page.goto('/o/acme/needs-you');
    const list = page.getByRole('list', { name: /Waiting on you/ });
    await expect(list).toBeVisible();
    await expect(page.getByRole('button', { name: /^Approve/ })).toHaveCount(0);

    const tops = await page
      .getByRole('group', { name: 'Show' })
      .getByRole('radio')
      .evaluateAll((buttons) => buttons.map((b) => Math.round(b.getBoundingClientRect().top)));
    expect(new Set(tops).size).toBe(1);

    await page.evaluate(() => window.scrollTo(0, 40));
    const before = await page.evaluate(() => window.scrollY);
    await list.getByRole('link').first().click();
    await expect(page).toHaveURL(/entry=/);
    await expect(list).toHaveCount(0);
    await page.getByRole('link', { name: 'Back to Needs you' }).click();
    await expect(list).toBeVisible();
    await expect(page).not.toHaveURL(/entry=/);
    await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(before); // the router restores it after it renders
    await expectNoHorizontalScroll(page);
  });
});

test('the sidebar Needs you link shows the page', async ({ page }) => {
  await page.goto('/o/acme');
  await openNavigation(page);
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

// Below 1024 px the page shows the list, then the entry, so the detail always has a full column.
test('at 768 px the filter row and the opened entry stay inside the screen', async ({ page }) => {
  await page.setViewportSize({ width: 768, height: 900 });
  await page.goto('/o/acme/needs-you');
  const list = page.getByRole('list', { name: /Waiting on you/ });
  await expect(list).toBeVisible();
  await expect(page.getByRole('button', { name: /All factories/ })).toBeVisible();
  await expectNoClippedElements(page);
  await expectNoHorizontalScroll(page);

  await list.getByRole('link').first().click();
  await expect(page.getByRole('link', { name: 'Back to Needs you' })).toBeVisible();
  await expectNoClippedElements(page);
  await expectNoHorizontalScroll(page);
});

test('at 390 px the tabs scroll sideways and Alerts can be selected', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/o/acme/needs-you');
  const alerts = page.getByRole('radio', { name: /Alerts/ });
  await alerts.evaluate((el) => el.parentElement?.scrollIntoView({ inline: 'center', block: 'nearest' }));
  await alerts.click({ force: true });
  await expect(page).toHaveURL(/type=alert/);
  const inView = await alerts.evaluate((el) => {
    const box = (el.parentElement as HTMLElement).getBoundingClientRect();
    return box.left >= 0 && box.right <= document.documentElement.clientWidth;
  });
  expect(inView).toBe(true);
  await expectNoHorizontalScroll(page);
  await expectNoClippedElements(page);
});

test('Undo this change on a watch_worse entry lands on the undo proposal', async ({ page }) => {
  // The mock seeds software's published proposal 5, whose sent-back rate got worse (mock/jonah.ts); the undo is
  // written as the proposal after the highest seeded one, so the test reads its number from the answer.
  await page.goto('/o/acme/needs-you');
  const list = page.getByRole('list', { name: /Waiting on you/ });
  await list.getByRole('link', { name: /Proposal 5 made .* worse/ }).click();

  const made = page.waitForResponse(
    (r) => r.url().endsWith('/jonah/proposals/5/undo') && r.request().method() === 'POST',
  );
  await page.getByRole('button', { name: /^Undo this change/ }).click();
  const proposal = (await (await made).json()) as { number: number; undoes: number };

  expect(proposal.undoes).toBe(5);
  await expect(page).toHaveURL(new RegExp(`/o/acme/f/software/jonah/proposals/${proposal.number}$`));
});

test('the three watch_worse buttons are the same height', async ({ page }) => {
  await page.goto('/o/acme/needs-you');
  const list = page.getByRole('list', { name: /Waiting on you/ });
  await list.getByRole('link', { name: /Proposal 5 made .* worse/ }).click();
  const heights = [
    await boxOf(page.getByRole('button', { name: /^Undo this change/ })),
    await boxOf(page.getByRole('link', { name: /^Open proposal \d+/ }).first()),
    await boxOf(page.getByRole('link', { name: /^Open the .* Floor$/ }).first()),
  ].map((box) => box.height);
  expect(new Set(heights).size).toBe(1);
});

// A used-up weekly budget: the mock caps the sre factory's week at a cent (POST /__mock/reset {usedUpBudget}).
const needsCount = async (page: import('@playwright/test').Page) => {
  await openNavigation(page);
  const text =
    (await orgNav(page)
      .getByRole('link', { name: /^Needs you/ })
      .first()
      .innerText()) ?? '';
  return Number(/\d+/.exec(text)?.[0] ?? 0);
};

test('a used-up weekly budget is an Alert with its resume time, counted in the sidebar, with the settings link', async ({
  page,
  request,
}) => {
  await page.goto('/o/acme/needs-you');
  await expect(page.getByRole('list', { name: /Waiting on you/ })).toBeVisible();
  const before = await needsCount(page);

  await request.post('/__mock/reset', { data: { usedUpBudget: 'sre' } });
  await page.goto('/o/acme/needs-you?type=alert');
  const list = page.getByRole('list', { name: /Waiting on you/ });
  const row = list.getByRole('link', { name: /Weekly budget used up · \$[\d,.]+ of \$0\.01/ });
  await expect(row).toBeVisible();
  await expect(row).toContainText(/Resumes about \w{3} \d{2}:\d{2}/);
  expect(await needsCount(page)).toBe(before + 1);
  await expectNoHorizontalScroll(page);
  await expectNoClippedElements(page);

  await row.click();
  const detail = page.getByRole('article');
  await expect(detail.getByRole('heading', { name: /^Weekly budget used up · / })).toBeVisible();
  await expect(detail.getByRole('button', { name: 'About the weekly budget' })).toBeVisible();
  await detail.getByRole('link', { name: 'Change the weekly budget' }).click();
  await expect(page).toHaveURL(/\/o\/acme\/f\/sre\/settings#budgets$/);
  await expect(page.locator('#budgets')).toBeInViewport();
});

test('someone who cannot edit the factory reads who can, with no link', async ({ page, request }) => {
  await request.post('/__mock/reset', { data: { usedUpBudget: 'sre', role: 'viewer' } });
  await page.goto('/o/acme/needs-you?type=alert');
  await page
    .getByRole('list', { name: /Waiting on you/ })
    .getByRole('link', { name: /Weekly budget used up/ })
    .click();
  const detail = page.getByRole('article');
  await expect(detail.getByText('Factory editors and organization admins can change the weekly budget.')).toBeVisible();
  await expect(detail.getByRole('link', { name: 'Change the weekly budget' })).toHaveCount(0);
  await expectNoHorizontalScroll(page);
});
