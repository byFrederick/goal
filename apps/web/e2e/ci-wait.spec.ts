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

async function patchBoard(page: Page, waiting: unknown) {
  await page.route(/\/factories\/[^/]+\/board(\?|$)/, async (route) => {
    const response = await route.fetch();
    const board = await response.json();
    const station = board.stations.find((s: { items: unknown[] }) => s.items.length > 0);
    Object.assign(station.items[0], { state: 'queued', waiting });
    await route.fulfill({ response, json: board });
  });
}

async function expectList(page: Page, label: string, failedWait: boolean) {
  await expect(page.getByText(label).first()).toBeVisible();
  await page.getByRole('button', { name: 'About CI' }).first().click();
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

for (const [name, waiting, label] of [
  ['running', running, 'Waiting for CI · 9 of 14'],
  ['failed', failed, 'CI failed: task web:e2e (3/12)'],
] as const) {
  test(`a ${name} CI wait reads ${label} on a Board card, with its ⓘ`, async ({ page }) => {
    await patchBoard(page, waiting);
    await page.goto('/o/acme/f/goal/board');
    await expectList(page, label, name === 'failed');
  });

  test(`a ${name} CI wait reads ${label} on the item page, with its ⓘ`, async ({ page }) => {
    await page.route(/\/factories\/[^/]+\/items\/[^/?]+(\?|$)/, async (route) => {
      const response = await route.fetch();
      const item = await response.json();
      Object.assign(item, { state: 'queued', waiting });
      await route.fulfill({ response, json: item });
    });
    await page.goto('/o/acme/f/goal/items/12');
    await expectList(page, label, name === 'failed');
  });
}
