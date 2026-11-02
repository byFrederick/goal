// The Runs page (/o/acme/runs) against the mock: the pictured rows, the URL filters and the controls
// that are not built yet. Not run in the box (no browser): CI runs it with the mock server.
import { expect, type Page, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll } from './support';

const runs = '/o/acme/runs';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

async function openRuns(page: Page, url = runs) {
  await page.goto(url);
  await expect(page.getByRole('heading', { level: 1, name: 'Runs' })).toBeVisible();
  await expect(page.getByRole('table')).toBeVisible();
}

test('shows the pictured rows and the four tiles', async ({ page }) => {
  await openRuns(page);

  await expect(page.getByText('Every time an agent worked on an item, in all factories.')).toBeVisible();
  await expect(page.getByRole('row', { name: /Fix flaky TestLeaseExpiry/ })).toContainText('Running');
  await expect(page.getByRole('row', { name: /Checkout API p95/ })).toContainText('Stopped');
  await expect(page.getByText(/running now$/)).toBeVisible();
  await expect(page.getByText(/^Showing \d+ of \d+ runs today\.$/)).toBeVisible();
});

test('reads the filters from the URL and writes the outcome back', async ({ page }) => {
  await openRuns(page, `${runs}?status=failed&factory=sre`);
  await expect(page.getByRole('button', { name: /Failed/ })).toHaveAttribute('aria-pressed', 'true');

  await page.getByRole('button', { name: /^All \d+/ }).click();
  await expect(page).toHaveURL(/\/o\/acme\/runs\?factory=sre$/);
});

test('downloads the runs CSV from Export CSV, and the page stays put', async ({ page }) => {
  await openRuns(page);

  const exportCsv = page.getByRole('button', { name: 'Export CSV' });
  await expect(exportCsv).not.toHaveAttribute('aria-disabled', 'true');
  const download = page.waitForEvent('download');
  await exportCsv.click();
  expect((await download).suggestedFilename()).toMatch(/^runs-\d{4}-\d{2}-\d{2}\.csv$/);
  await expect(page).toHaveURL(/\/o\/acme\/runs$/);
});

test('keeps the controls that are not built yet disabled', async ({ page }) => {
  await openRuns(page);

  // Waiting on you is not built yet: disabled, and it does nothing.
  const waiting = page.getByRole('button', { name: /Waiting on you/ }).first();
  await expect(waiting).toHaveAttribute('aria-disabled', 'true');
  await waiting.click();
  await expect(page).toHaveURL(/\/o\/acme\/runs$/);
});

const columns = ['Factory', 'Item', 'Station', 'Agent', 'Model', 'Started', 'Duration', 'Turns', 'Cost', 'Outcome'];

for (const width of [1280, 1440]) {
  test(`shows every column at ${width} px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await openRuns(page);

    const box = await page.getByRole('table').locator('xpath=..').boundingBox();
    for (const name of columns) {
      const header = page.getByRole('columnheader', { name, exact: true });
      await expect(header).toBeVisible();
      const at = await header.boundingBox();
      expect((at?.x ?? 0) + (at?.width ?? 0)).toBeLessThanOrEqual((box?.x ?? 0) + (box?.width ?? 0) + 1);
    }
  });
}

for (const width of [375, 390]) {
  test(`has the Runs title, an Export CSV icon and no sideways scroll at ${width} px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await openRuns(page);

    await expect(page.getByRole('banner').getByText('Runs', { exact: true })).toBeVisible();
    const exportCsv = page.getByRole('button', { name: 'Export CSV' });
    expect((await exportCsv.boundingBox())?.height).toBeGreaterThanOrEqual(44);
    await expectNoHorizontalScroll(page);
  });
}

for (const scheme of ['light', 'dark'] as const) {
  for (const width of [1440, 390]) {
    test(`screenshot at ${width} px, ${scheme}`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: scheme });
      await page.setViewportSize({ width, height: 900 });
      await openRuns(page);
      await page.screenshot({ path: `test-results/runs-${width}-${scheme}.png`, fullPage: true });
    });
  }
}

for (const width of [375, 768, 1440]) {
  test(`fits ${width} px with the table in its own scroll box`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await openRuns(page);

    await expectNoHorizontalScroll(page);
    await expectAccessible(page);
  });
}

test('renders in dark mode', async ({ page }) => {
  await page.emulateMedia({ colorScheme: 'dark' });
  await openRuns(page);

  await expectAccessible(page);
});
