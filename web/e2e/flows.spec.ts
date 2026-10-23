import AxeBuilder from '@axe-core/playwright';
import { expect, type Page, test } from '@playwright/test';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

const isPhone = (page: Page) => (page.viewportSize()?.width ?? 0) < 768;

/** Below 1024 px the item page shows the latest steps; the whole trace is one tap away. */
async function showTrace(page: Page) {
  const button = page.getByRole('button', { name: 'Full trace' });
  if (await button.isVisible()) await button.click();
}

async function expectAccessible(page: Page) {
  const { violations } = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'])
    .analyze();
  expect(violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).join(', ')}`)).toEqual([]);
}

async function expectNoHorizontalScroll(page: Page) {
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  expect(overflow).toBeLessThanOrEqual(0);
}

for (const [name, path, heading] of [
  ['floor', '/f/goal', 'Goal floor'],
  ['board', '/f/goal/board', 'Goal board'],
  ['item', '/f/goal/items/6', 'Floor: throughput bars per day'],
  ['items', '/f/goal/items', 'Goal items'],
] as const) {
  test(`${name} is accessible and fits the screen`, async ({ page }) => {
    await page.goto(path);
    await expect(page.getByRole('heading', { level: 1, name: heading })).toBeVisible();
    await expectAccessible(page);
    await expectNoHorizontalScroll(page);
  });
}

test('the root opens the floor of the first factory', async ({ page }) => {
  await page.goto('/');
  await expect(page).toHaveURL(/\/f\/goal$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Goal floor' })).toBeVisible();
});

test('approve an item waiting on you from the floor', async ({ page }) => {
  await page.goto('/f/goal');
  const needs = page.getByRole('region', { name: 'Needs you' });
  await needs.getByRole('button', { name: 'Approve goal#12' }).click();

  await expect(page.getByText('Approved goal#12. It is now at Build.')).toBeVisible();
  await expect(needs.getByRole('link', { name: /goal#12/ })).toHaveCount(0);

  await page.goto('/f/goal/items/12');
  await expect(page.getByText('At Build', { exact: true })).toBeVisible();
  await showTrace(page);
  await expect(page.getByText('web approved')).toBeVisible();
});

test('send an item back with a reason', async ({ page }) => {
  await page.goto('/f/goal/items/12');
  await page.getByRole('button', { name: 'Send back' }).click();

  const dialog = page.getByRole('dialog', { name: 'Send goal#12 back' });
  await dialog.getByRole('button', { name: 'Send back' }).click();
  await expect(dialog.getByText('Write a reason so the agent knows what to fix.')).toBeVisible();
  await expectAccessible(page);

  await dialog.getByLabel('What must change').fill('Cover the empty-bucket case with a test.');
  await dialog.getByRole('button', { name: 'Send back' }).click();

  await expect(dialog).toBeHidden();
  await expect(page.getByText('Sent goal#12 back. It is now at Spec.')).toBeVisible();
  await expect(page.getByText('At Spec', { exact: true })).toBeVisible();
  await showTrace(page);
  await expect(page.getByText('Cover the empty-bucket case with a test.')).toBeVisible();
});

test('merge a pull request at the merge station', async ({ page }) => {
  await page.goto('/f/goal/items/6');
  await page.getByRole('button', { name: 'Merge' }).click();

  const dialog = page.getByRole('dialog', { name: 'Merge PR #38?' });
  await expect(dialog.getByRole('link', { name: /Review PR #38 first/ })).toHaveAttribute(
    'href',
    'https://github.com/acme/goal/pull/38',
  );
  await expectAccessible(page);
  await dialog.getByRole('button', { name: 'Squash and merge' }).click();

  await expect(page.getByText('Merged goal#6. It is done.')).toBeVisible();
  await expect(page.getByText('At Done', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Merge' })).toHaveCount(0);
});

test('retry a blocked item', async ({ page }) => {
  await page.goto('/f/goal/items/5');
  await expect(page.getByText('Blocked, needs a retry').first()).toBeVisible();
  await page.getByRole('button', { name: 'Retry' }).click();

  await expect(page.getByText('goal#5 is back in the queue.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Retry' })).toHaveCount(0);
});

test('the board updates live when an item moves elsewhere', async ({ page, request }) => {
  test.skip(isPhone(page), 'the columns are the desktop board');
  await page.goto('/f/goal/board');
  const approveSpec = page.getByRole('region', { name: 'Approve spec', exact: true });
  await expect(approveSpec.getByRole('link', { name: 'goald: serve the embedded web UI' })).toBeVisible();
  await expect(page.getByText('Live', { exact: true }).first()).toBeVisible();

  // Someone else approves (the CLI, another tab): the stream brings the change.
  await request.post('/v1/items/goal%2312/approve', { data: { by: 'cli' } });

  await expect(approveSpec.getByRole('link', { name: 'goald: serve the embedded web UI' })).toHaveCount(0);
  const build = page.getByRole('region', { name: 'Build', exact: true });
  await expect(build.getByRole('link', { name: 'goald: serve the embedded web UI' })).toBeVisible();
});

test('on a phone: floor to board to item and back', async ({ page }) => {
  test.skip(!isPhone(page), 'phone navigation');
  await page.goto('/f/goal');
  const tabs = page.getByRole('navigation', { name: 'Main' });

  await tabs.getByRole('link', { name: 'Board' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Goal board' })).toBeVisible();
  await expect(page.getByRole('tab', { name: /Build/ })).toHaveAttribute('aria-selected', 'true');

  await page.getByRole('tab', { name: /Merge/ }).click();
  await expect(page).toHaveURL(/station=merge/);
  await page.getByRole('link', { name: 'Floor: throughput bars per day' }).click();

  await expect(page.getByRole('heading', { level: 1, name: 'Floor: throughput bars per day' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Merge' })).toBeVisible();

  await page.getByRole('link', { name: 'Goal board' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Goal board' })).toBeVisible();

  await tabs.getByRole('link', { name: 'Floor' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Goal floor' })).toBeVisible();
});
