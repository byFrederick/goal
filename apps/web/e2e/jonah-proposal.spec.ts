// Proposal 7's page (jonah/proposal.jpg): it shows the picture's files and runs, Approve publishes it, Edit opens the
// draft and Reject records a reason. Not run in the box: Playwright needs a browser, which the box does not have.
import { expect, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll } from './support';
import { expectNoOverlap, expectTarget44 } from './support-boxes';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

test('the page shows proposal 7 as pictured: the estimate, the two diffs, the four runs and the details', async ({
  page,
}) => {
  await page.goto('/o/acme/f/software/jonah/proposals/7');
  await expect(page.getByRole('heading', { level: 1, name: /Ask frontend-engineer for a screenshot/ })).toBeVisible();
  await expect(page.getByText('Waiting for your review', { exact: true }).first()).toBeVisible();
  await expect(page.getByText('2 files, v21 to v22')).toBeVisible();
  await expect(page.getByRole('region', { name: /Why/ })).toBeVisible();
  // Details is shut below 768 px (proposal-page.tsx DetailsCard): open it before reading its rows.
  const publishes = page.getByText('v22; running items keep v21');
  if ((page.viewportSize()?.width ?? 1440) < 768) await page.getByRole('button', { name: 'Details' }).click();
  await expect(publishes).toBeVisible();

  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('the memory proposal shows its facts, the people it quotes and that no try ran', async ({ page }) => {
  await page.goto('/o/acme/f/software/jonah/proposals/9');
  await expect(
    page.getByRole('heading', { level: 1, name: /Add 3 facts from answers, notes and send-backs/ }),
  ).toBeVisible();
  await expect(page.getByText('Waiting for your review', { exact: true }).first()).toBeVisible();
  await expect(page.getByText('Changes to factory memory')).toBeVisible();
  await expect(page.getByText('Each quotes a person')).toBeVisible();
  await expect(page.getByText('Memory changes run no try')).toBeVisible();

  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('the model proposal shows its tries, the line result and the tried items', async ({ page }) => {
  await page.goto('/o/acme/f/software/jonah/proposals/10');
  await expect(page.getByRole('heading', { level: 1, name: 'frontend-engineer on claude-haiku-5-5' })).toBeVisible();
  await expect(page.getByText('Waiting for your review', { exact: true }).first()).toBeVisible();
  await expect(page.getByText('Tries passed', { exact: true })).toBeVisible();
  await expect(page.getByText('11 of 21', { exact: true }).first()).toBeVisible();
  await expect(page.getByText('On the line', { exact: true }).first()).toBeVisible();
  await expect(page.getByRole('columnheader', { name: 'On the line' })).toBeVisible();
  await expect(page.getByText('5 of 7', { exact: true }).first()).toBeVisible();
  await expect(page.getByText('Tried on past items', { exact: true }).first()).toBeVisible();
  const table = page.getByRole('table', { name: 'Tries per item' });
  await expect(table.getByRole('rowheader', { name: /Phone fixes/ })).toBeVisible();

  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('Approve publishes proposal 7 and opens the definition at its versions', async ({ page }) => {
  await page.goto('/o/acme/f/software/jonah/proposals/7');
  await page.getByRole('button', { name: 'Approve and publish v22' }).click();
  await expect(page).toHaveURL(/\/definition\/versions/);
});

// The approve POST alone is delayed with the mock's latency header, so the busy state stays on screen.
// Runs in each Playwright project (375, 768, 1440 px) and in both colour schemes. Not run in the box: CI-verified.
for (const scheme of ['light', 'dark'] as const) {
  test(`Approve shows Publishing v22 while it waits, then the Published check (${scheme})`, async ({ page }) => {
    // The config sets reducedMotion: 'reduce', which makes the check's hold time 0 and the page navigates at once.
    await page.emulateMedia({ colorScheme: scheme, reducedMotion: 'no-preference' });
    await page.route('**/proposals/7/approve', (route) =>
      route.continue({ headers: { ...route.request().headers(), 'x-mock-latency-ms': '1500' } }),
    );
    await page.goto('/o/acme/f/software/jonah/proposals/7');
    // The accessible name changes with the state (idle, busy, done), so one locator matches all three.
    const button = page.getByRole('button', { name: /^(Approve and publish v22|Publishing v22…|Published)$/ });
    await expect(button).toHaveAccessibleName('Approve and publish v22');
    const before = (await button.boundingBox())?.width ?? 0;

    await button.click();
    await expect(button).toHaveAttribute('aria-busy', 'true');
    await expect(button).toHaveAccessibleName('Publishing v22…');
    await expect(button.locator('[data-slot="busy"] .animate-spin')).toBeVisible();
    const during = (await button.boundingBox())?.width ?? 0;
    expect(Math.abs(during - before)).toBeLessThanOrEqual(1);

    await expect(button).toHaveAccessibleName('Published');
    await expect(page).toHaveURL(/\/definition\/versions/);
  });
}

test('Approve shows the draft-open message on the flagged proposal 8', async ({ page }) => {
  await page.goto('/o/acme/f/software/jonah/proposals/8');
  await page.getByRole('button', { name: 'Approve and publish v22' }).click();
  await expect(page.getByText(/The definition already has an open draft/)).toBeVisible();
});

test('Reject needs a reason, then records it and shows the rejected state', async ({ page }) => {
  await page.goto('/o/acme/f/software/jonah/proposals/7');
  // Below 768 px the top bar keeps only the primary action; Reject sits behind More actions (phone-chrome.tsx).
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
  if ((page.viewportSize()?.width ?? 1440) < 768) {
    await page.getByRole('button', { name: 'More actions' }).click();
    await page.getByRole('menuitem', { name: 'Reject with a reason' }).click();
  } else {
    await page.getByRole('button', { name: 'Reject with a reason' }).click();
  }
  const dialog = page.getByRole('dialog', { name: 'Reject with a reason' });
  const submit = dialog.getByRole('button', { name: 'Reject proposal' });
  await expect(submit).toBeDisabled();

  await dialog.getByRole('textbox', { name: 'Reason' }).fill('Put the screenshot rule in the pull request template.');
  await expect(submit).toBeEnabled();
  await submit.click();

  await expect(page.getByText(/^Rejected/).first()).toBeVisible();
  await expect(page.getByRole('button', { name: 'Approve and publish v22' })).toHaveCount(0);
});

test('Try again shows the estimate, and Confirm adds a try to each item', async ({ page }) => {
  await page.goto('/o/acme/f/software/jonah/proposals/7');
  await page.getByRole('button', { name: 'Try again' }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog.getByText(/Adds 2 tries for about \$2\.80/)).toBeVisible();
  await dialog.getByRole('button', { name: 'Queue the tries' }).click();
  await expect(page.getByText('Try 2 of 2').first()).toBeVisible();
});

test('Try again is refused with Not tried: budget used once the budget is gone', async ({ page }) => {
  await page.goto('/o/acme/f/software/jonah/proposals/7');
  for (let round = 0; round < 2; round++) {
    await page.getByRole('button', { name: 'Try again' }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Queue the tries' }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
  }
  await page.getByRole('button', { name: 'Try again' }).click();
  await expect(page.getByRole('dialog').getByText('Not tried: budget used')).toBeVisible();
});

test('Try again is 44 px at 390 px and the card fits at 390, 768 and 1440 px', async ({ page }) => {
  for (const [width, height] of [
    [1440, 900],
    [768, 1024],
    [390, 844],
  ] as const) {
    await page.setViewportSize({ width, height });
    await page.goto('/o/acme/f/software/jonah/proposals/7');
    const button = page.getByRole('button', { name: 'Try again' });
    await expect(button).toBeVisible();
    if (width === 390) expect((await button.boundingBox())?.height ?? 0).toBeGreaterThanOrEqual(44);
    await expectNoHorizontalScroll(page);
  }
});

test('the page fits 375 and 768 px and renders in dark mode', async ({ page }) => {
  for (const width of [375, 768]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/o/acme/f/software/jonah/proposals/7');
    await expect(page.getByText('Waiting for your review', { exact: true }).first()).toBeVisible();
    await expectNoHorizontalScroll(page);
  }

  await page.setViewportSize({ width: 1440, height: 900 });
  await page.emulateMedia({ colorScheme: 'dark', reducedMotion: 'reduce' });
  await page.goto('/o/acme/f/software/jonah/proposals/7');
  await expect(page.getByText('Waiting for your review', { exact: true }).first()).toBeVisible();
  await expectAccessible(page);
});

// Screenshots of proposal 7 at 1440 and 375 px in light and dark, for the pull request.
for (const scheme of ['light', 'dark'] as const) {
  for (const width of [1440, 375]) {
    test(`screenshot: proposal 7 at ${width} px, ${scheme}`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: scheme });
      await page.setViewportSize({ width, height: 900 });
      await page.goto('/o/acme/f/software/jonah/proposals/7');
      await expect(page.getByRole('region', { name: 'Expected effect' })).toBeVisible();
      await expectNoHorizontalScroll(page);
      await page.screenshot({ path: `test-results/jonah-proposal-${width}-${scheme}.png`, fullPage: true });
    });
  }
}

test('the tries ⓘ is 44×44 and does not lie over Try again at 390 px', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/o/acme/f/software/jonah/proposals/7');
  const again = page.getByRole('button', { name: 'Try again' });
  await expect(again).toBeVisible();
  for (const name of ['How tries work', 'How model tries work']) {
    const tip = page.getByRole('button', { name });
    if ((await tip.count()) === 0) continue;
    await expectTarget44(tip, name);
    await expectNoOverlap(tip, again, `${name} and Try again`);
  }
});

// Not run in the box: Playwright needs a browser, which the box does not have.
for (const width of [375, 768, 1440]) {
  test(`a model proposal shows no evidence lines at ${width} px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/o/acme/f/software/jonah/proposals');
    const card = page.getByRole('article', { name: /frontend-engineer on claude-haiku-5-5/ });
    await expect(card).toBeVisible();
    await expect(card.getByText(/runs sent back/)).toHaveCount(0);

    await page.goto('/o/acme/f/software/jonah/proposals/10');
    await expect(page.getByRole('heading', { level: 1, name: /frontend-engineer on claude-haiku-5-5/ })).toBeVisible();
    await expect(page.getByText('Found in')).toHaveCount(0);
    await expect(page.getByRole('heading', { name: /^Why/ })).toHaveCount(0);
    await expect(page.getByText(/runs sent back/)).toHaveCount(0);
  });
}

test('Reject with a reason works from More actions at 375 px', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto('/o/acme/f/software/jonah/proposals/7');
  await page.getByRole('button', { name: 'More actions' }).click();
  await page.getByRole('menuitem', { name: 'Reject with a reason' }).click();
  const dialog = page.getByRole('dialog', { name: 'Reject with a reason' });
  await expect(dialog).toBeVisible();
  await dialog.getByRole('textbox', { name: 'Reason' }).fill('Put the screenshot rule in the pull request template.');
  await dialog.getByRole('button', { name: 'Reject proposal' }).click();
  await expect(page.getByText(/^Rejected/).first()).toBeVisible();
});

test('at 768 px the last crumb stays visible and overlaps no action', async ({ page }) => {
  await page.setViewportSize({ width: 768, height: 900 });
  await page.goto('/o/acme/f/software/jonah/proposals/7');
  const bar = page.getByRole('banner');
  const crumb = await bar.getByText('Proposal 7', { exact: true }).boundingBox();
  expect(crumb).not.toBeNull();
  expect(crumb?.width ?? 0).toBeGreaterThan(40);
  for (const name of ['Edit', 'Reject with a reason', 'Approve and publish v22']) {
    const box = await bar
      .getByRole('button', { name })
      .or(bar.getByRole('link', { name, exact: true }))
      .first()
      .boundingBox();
    expect(box).not.toBeNull();
    const apart =
      (crumb?.x ?? 0) + (crumb?.width ?? 0) <= (box?.x ?? 0) || (crumb?.y ?? 0) + (crumb?.height ?? 0) <= (box?.y ?? 0);
    expect(apart).toBe(true);
  }
});
