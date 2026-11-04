// Jonah's three tabs (jonah/jonah.jpg, jonah/tests.jpg, mobile/jonah.jpg): the Overview with the proposals waiting,
// the policy and what Jonah noticed; the Proposals tab's state filter; the Tests tab's kept tests by station.
// Not run in the box: Playwright needs a browser, which the box does not have.
import { expect, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll } from './support';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

test('the overview shows the proposals waiting, the policy and what Jonah noticed', async ({ page }) => {
  await page.goto('/o/acme/f/software/jonah');
  await expect(page.getByRole('heading', { level: 1, name: 'Jonah' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Waiting for your review' })).toBeVisible();

  await expect(page.getByText('Proposal 7')).toBeVisible();
  await expect(page.getByText('Proposal 8')).toBeVisible();
  await expect(page.getByText('Based on 4 runs sent back')).toBeVisible();
  await expect(page.getByText('Watching, 3 of 5')).toBeVisible();
  await expect(page.getByText('Review sends back 1 in 4 frontend items for a missing phone check')).toBeVisible();
  await expect(page.getByText(/Plain-language judge/)).toHaveCount(0);
  await expect(page.getByRole('region', { name: /After publish/ })).toBeVisible();

  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('the tabs are Overview, Proposals and Tests, each at its own address', async ({ page }) => {
  await page.goto('/o/acme/f/software/jonah');
  const tabs = page.getByRole('navigation', { name: 'Jonah' });
  await expect(tabs.getByRole('link')).toHaveText([/Overview/, /Proposals/, /Tests/]);
  await tabs.getByRole('link', { name: /Proposals/ }).click();
  await expect(page).toHaveURL(/\/jonah\/proposals$/);
  await tabs.getByRole('link', { name: /Tests/ }).click();
  await expect(page).toHaveURL(/\/jonah\/tests$/);
  await expect(tabs.getByRole('link', { name: /Tests/ })).toHaveAttribute('aria-current', 'page');
});

test('each proposal links to its page', async ({ page }) => {
  await page.goto('/o/acme/f/software/jonah');
  await expect(page.getByRole('link', { name: /^Review/ }).first()).toHaveAttribute('href', /\/jonah\/proposals\/10$/);
});

test('Look for improvements now queues a review, and a second one is refused', async ({ page }) => {
  await page.goto('/o/acme/f/software/jonah');
  const look = page.getByRole('button', { name: 'Look for improvements now' });
  await look.click();
  await expect(page.getByText('Jonah is looking. New proposals appear here.')).toBeVisible();
  await look.click();
  await expect(page.getByText('A review is already running. Its proposals appear here.')).toBeVisible();
});

test('on a phone the review button keeps its name, is 44 px and its text does not spill', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 800 });
  await page.goto('/o/acme/f/software/jonah');
  const look = page.getByRole('button', { name: 'Look for improvements now' });
  const box = await look.boundingBox();
  expect(box?.width).toBeGreaterThanOrEqual(44);
  expect(box?.height).toBeGreaterThanOrEqual(44);
  const spills = await look.evaluate((el) =>
    [...el.querySelectorAll('*')].some((c) => c.scrollWidth > c.clientWidth && c.clientWidth > 0),
  );
  expect(spills).toBe(false);
});

test('the Proposals tab filters by the state in the URL', async ({ page }) => {
  await page.goto('/o/acme/f/software/jonah/proposals');
  await expect(
    page.getByRole('navigation', { name: 'Jonah' }).getByRole('link', { name: /Proposals/ }),
  ).toHaveAttribute('aria-current', 'page');
  await page.getByRole('radio', { name: 'Applied' }).check();
  await expect(page).toHaveURL(/state=applied/);
  await expect(page.getByText('Name the agent in every sent-back note')).toBeVisible();
});

test('the Tests tab lists kept tests by station, and Remove takes one away', async ({ page }) => {
  await page.goto('/o/acme/f/software/jonah/tests');
  await expect(page.getByRole('region', { name: 'build tests' })).toBeVisible();
  await expect(page.getByText('Needs you header in the top bar')).toBeVisible();
  await page.getByRole('button', { name: 'Actions for test #418' }).click();
  await page.getByRole('menuitem', { name: 'Remove' }).click();
  await expect(page.getByText('Needs you header in the top bar')).toHaveCount(0);
  await expectAccessible(page);
});

test('the Tests tab shows each kept test’s last result and links a run to its item', async ({ page }) => {
  await page.goto('/o/acme/f/software/jonah/tests');
  const kept = page.getByRole('region', { name: 'Kept tests' });
  await expect(kept.getByText('Passed')).toBeVisible();
  await expect(kept.getByText('Failed')).toBeVisible();
  await expect(kept.getByText('Error', { exact: true })).toBeVisible();
  await expect(kept.getByText('Not run yet')).toBeVisible();
  await kept.getByRole('link', { name: /Passed on/ }).click();
  await expect(page).toHaveURL(/\/items\/451\?tab=trace/);
});

test('the Tests tab’s last results fit 390, 768 and 1440 px, and every control is 44 px at 390', async ({ page }) => {
  for (const [width, height] of [
    [1440, 900],
    [768, 1024],
    [390, 844],
  ] as const) {
    await page.setViewportSize({ width, height });
    await page.goto('/o/acme/f/software/jonah/tests');
    await expect(page.getByText('Not run yet')).toBeVisible();
    await expectNoHorizontalScroll(page);
    if (width === 390) {
      const targets = page.getByRole('region', { name: 'Kept tests' }).locator('a, button');
      for (const box of await targets.evaluateAll((els) => els.map((e) => e.getBoundingClientRect().height))) {
        expect(box).toBeGreaterThanOrEqual(44);
      }
    }
  }
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

// Every Jonah page at 1440, 1024, 768 and 390 px in both themes: nothing wider than the screen, and a screenshot.
const jonahPages = [
  ['overview', '/o/acme/f/software/jonah', 'Overview'],
  ['proposals', '/o/acme/f/software/jonah/proposals', 'Proposals'],
  ['tests', '/o/acme/f/software/jonah/tests', 'Tests'],
] as const;

for (const scheme of ['light', 'dark'] as const) {
  for (const width of [1440, 1024, 768, 390]) {
    for (const [name, url, tab] of jonahPages) {
      test(`${name} at ${width} px in ${scheme}: current tab, no sideways scroll, screenshot`, async ({ page }) => {
        await page.setViewportSize({ width, height: 900 });
        await page.emulateMedia({ colorScheme: scheme, reducedMotion: 'reduce' });
        await page.goto(url);
        const tabs = page.getByRole('navigation', { name: 'Jonah' });
        await expect(tabs.locator('[aria-current="page"]')).toHaveCount(1);
        await expect(tabs.getByRole('link', { name: new RegExp(tab) })).toHaveAttribute('aria-current', 'page');
        await expectNoHorizontalScroll(page);
        await expect(page).toHaveScreenshot(`jonah-${name}-${width}-${scheme}.png`);
      });
    }
  }
}
