// A CI wait on a Board card and on the item page: the label, and the ⓘ that lists commits and checks as links.
// The mock server has no CI wait, so the board and item answers are patched with page.route.
import { expect, type Page, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll } from './support';

const repo = 'github.com/acme/app';
const check = (over: Record<string, unknown>) => ({ repo, failed_jobs: [], ...over });
const running = {
  kind: 'ci',
  items: [],
  files: [],
  needs: [],
  ci: {
    commits: [{ repo, sha: '0123456789abcdef', url: 'https://github.com/acme/app/pull/7' }],
    checks: [
      check({ name: 'build', state: 'finished', result: 'passed', url: 'https://ci.example.com/build' }),
      check({ name: 'task web:e2e', state: 'running', url: 'https://ci.example.com/e2e' }),
    ],
    finished: 9,
    total: 14,
  },
};
const failed = {
  ...running,
  ci: {
    ...running.ci,
    failure: {
      repo,
      sha: 'fedcba9876543210',
      url: 'https://github.com/acme/app/pull/6',
      checks: [
        check({
          name: 'task web:e2e',
          state: 'finished',
          result: 'failed',
          failed_jobs: ['3/12'],
          url: 'https://ci.example.com/e2e-failed',
        }),
      ],
    },
  },
};

// Patches the first station that has items and returns a getter for its id: a phone shows one station per tab.
async function patchBoard(page: Page, waiting: unknown): Promise<() => string | undefined> {
  let patched: string | undefined;
  await page.route(/\/factories\/[^/]+\/board(\?|$)/, async (route) => {
    const response = await route.fetch();
    const board = await response.json();
    const station = board.stations.find((s: { items: unknown[] }) => s.items.length > 0);
    Object.assign(station.items[0], { state: 'queued', waiting });
    patched = station.id;
    await route.fulfill({ response, json: board });
  });
  return () => patched;
}

// On a phone, picks the patched station's tab so its column is the one shown.
async function showStation(page: Page, station: () => string | undefined) {
  await expect.poll(station).toBeDefined();
  const tab = page.locator(`[role="tab"][data-station="${station()}"]`);
  if (await tab.isVisible()) await tab.click();
}

async function expectList(page: Page, label: string, failedWait: boolean) {
  await expect(page.getByText(label).filter({ visible: true }).first()).toBeVisible();
  await page.getByRole('button', { name: 'About CI' }).filter({ visible: true }).first().click();
  if (failedWait) {
    const failed = page.getByRole('group', { name: `${repo}, failed` });
    await expect(failed.getByRole('link', { name: 'fedcba9' })).toHaveAttribute(
      'href',
      'https://github.com/acme/app/pull/6',
    );
    await expect(failed.getByRole('link', { name: 'task web:e2e' })).toHaveAttribute(
      'href',
      'https://ci.example.com/e2e-failed',
    );
    await expect(failed.getByText('3/12')).toBeVisible();
  }
  const group = page.getByRole('group', { name: repo, exact: true });
  await expect(group.getByRole('link', { name: '0123456' })).toHaveAttribute(
    'href',
    'https://github.com/acme/app/pull/7',
  );
  await expect(group.getByRole('link', { name: 'build' })).toHaveAttribute('href', 'https://ci.example.com/build');
  await expectNoHorizontalScroll(page);
  await expectAccessible(page);
}

for (const [name, waiting, label, long] of [
  ['running', running, 'CI · 9 of 14', 'Waiting for CI · 9 of 14'],
  ['failed', failed, 'CI failed:', 'CI failed: task web:e2e (3/12)'],
] as const) {
  for (const width of [375, 768, 1440]) {
    test(`a ${name} CI wait keeps ${label} untruncated on a Board card at ${width} px with 7 stations`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 });
      let patched: string | undefined;
      await page.route(/\/factories\/[^/]+\/board(\?|$)/, async (route) => {
        const response = await route.fetch();
        const board = await response.json();
        // Pad the board to 7 stations so each column is as narrow as production's.
        while (board.stations.length < 7) {
          const copy = structuredClone(board.stations[board.stations.length - 1]);
          copy.id = `${copy.id}-extra${board.stations.length}`;
          copy.items = [];
          board.stations.push(copy);
        }
        const station = board.stations.find((s: { items: unknown[] }) => s.items.length > 0);
        Object.assign(station.items[0], { state: 'queued', waiting });
        patched = station.id;
        await route.fulfill({ response, json: board });
      });
      await page.goto('/o/acme/f/goal/board');
      await showStation(page, () => patched);
      const text = page.getByText(label, { exact: true }).filter({ visible: true }).first();
      await expect(text).toBeVisible();
      const fits = await text.evaluate((el) => el.scrollWidth <= el.clientWidth);
      expect(fits).toBe(true);
      await expect(page.locator(`[title="${long}"]`).filter({ visible: true }).first()).toBeVisible();
    });
  }

  test(`a ${name} CI wait has its ⓘ on a Board card`, async ({ page }) => {
    const station = await patchBoard(page, waiting);
    await page.goto('/o/acme/f/goal/board');
    await showStation(page, station);
    await expectList(page, label, name === 'failed');
  });

  test(`a ${name} CI wait reads ${long} on the item page, with its ⓘ`, async ({ page }) => {
    // The item page reads /v1/orgs/{org}/items/{ref}; the pattern leaves out /events, /runs and other subpaths.
    await page.route(/\/v1\/orgs\/[^/]+\/(factories\/[^/]+\/)?items\/[^/?]+(\?|$)/, async (route) => {
      const response = await route.fetch();
      const item = await response.json();
      Object.assign(item, { state: 'queued', waiting });
      await route.fulfill({ response, json: item });
    });
    await page.goto('/o/acme/f/goal/items/12');
    await expectList(page, long, name === 'failed');
  });
}
