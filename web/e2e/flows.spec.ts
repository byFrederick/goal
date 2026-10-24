import { expect, type Page, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll, isPhone, orgNav } from './support';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

/** On a narrow page the item shows its latest steps; the whole trace is one tap away. */
async function showTrace(page: Page) {
  const button = page.getByRole('button', { name: 'Full trace' });
  if (await button.isVisible()) await button.click();
}

for (const [name, path, heading] of [
  ['factories', '/o/acme', 'Factories'],
  ['floor', '/o/acme/f/goal', 'Goal floor'],
  ['board', '/o/acme/f/goal/board', 'Goal board'],
  ['item', '/o/acme/f/goal/items/6', 'Floor: throughput bars per day'],
  ['items', '/o/acme/f/goal/items', 'Goal items'],
  ['another factory', '/o/acme/f/sre', 'SRE floor'],
] as const) {
  test(`${name} is accessible and fits the screen`, async ({ page }) => {
    await page.goto(path);
    await expect(page.getByRole('heading', { level: 1, name: heading })).toBeVisible();
    await expect(orgNav(page)).toBeVisible();
    await expectAccessible(page);
    await expectNoHorizontalScroll(page);
  });
}

test('the root opens the factories of the first organization', async ({ page }) => {
  await page.goto('/');
  await expect(page).toHaveURL(/\/o\/acme$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Factories' })).toBeVisible();
  await expect(page.getByRole('article')).toHaveCount(3);
  await expect(page).toHaveTitle('Factories · Acme · Goal');
});

test('the root returns to the organization used last', async ({ page }) => {
  await page.goto('/o/example-labs');
  await expect(page.getByRole('heading', { level: 1, name: 'Factories' })).toBeVisible();
  await page.goto('/');
  await expect(page).toHaveURL(/\/o\/example-labs$/);
  await expect(page.getByRole('article')).toHaveCount(1);
  await expect(orgNav(page).getByRole('link', { name: /factory/ })).toHaveCount(1);
});

test('old /f/ links move under their organization, keeping search params', async ({ page }) => {
  await page.goto('/f/goal/items/12');
  await expect(page).toHaveURL(/\/o\/acme\/f\/goal\/items\/12$/);
  await expect(page.getByRole('heading', { level: 1, name: 'goald: serve the embedded web UI' })).toBeVisible();

  await page.goto('/f/research/items?state=done');
  await expect(page).toHaveURL(/\/o\/example-labs\/f\/research\/items\?state=done$/);

  await page.goto('/f/nothing-here');
  await expect(page.getByRole('heading', { level: 1, name: 'No such factory' })).toBeVisible();
});

test('an unknown organization or factory says so', async ({ page }) => {
  await page.goto('/o/nobody');
  await expect(page.getByRole('heading', { level: 1, name: 'No such organization' })).toBeVisible();

  await page.goto('/o/acme/f/nothing-here');
  await expect(page.getByRole('heading', { level: 1, name: 'No such factory' })).toBeVisible();
  await expect(orgNav(page)).toBeVisible();
});

test('pages are titled most specific first', async ({ page }) => {
  await page.goto('/o/acme/f/sre/board');
  await expect(page).toHaveTitle('Board · sre · Acme · Goal');
  await page.goto('/o/acme/f/goal/items/12');
  await expect(page).toHaveTitle('goal#12 · goal · Acme · Goal');
});

test('the rail moves by arrow keys and opens a factory', async ({ page }) => {
  test.skip(isPhone(page), 'the rail is the 768 px and wider navigation');
  await page.goto('/o/acme');
  await expect(page.getByRole('heading', { level: 1, name: 'Factories' })).toBeVisible();

  const rail = orgNav(page);
  const mark = rail.getByRole('link', { name: 'Ac, Acme organization, factories' });
  await page.keyboard.press('Tab'); // skip link
  await page.keyboard.press('Tab');
  await expect(mark).toBeFocused();

  await page.keyboard.press('ArrowDown');
  await page.keyboard.press('ArrowDown');
  const sre = rail.getByRole('link', { name: 'S, sre factory, 3 waiting on you' });
  await expect(sre).toBeFocused();
  const tip = page.locator('[data-radix-popper-content-wrapper]');
  await expect(tip).toContainText('sre');
  await expect(tip).toContainText('3 waiting on you');

  await page.keyboard.press('End');
  await expect(rail.getByRole('button', { name: 'Credentials' })).toBeFocused();
  await page.keyboard.press('Home');
  await expect(mark).toBeFocused();

  await page.keyboard.press('ArrowDown');
  await page.keyboard.press('ArrowDown');
  await page.keyboard.press('Enter');
  await expect(page).toHaveURL(/\/o\/acme\/f\/sre$/);
  await expect(sre).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('heading', { level: 1, name: 'SRE floor' })).toBeFocused();
  await expect(
    page.getByRole('navigation', { name: 'sre factory' }).getByRole('link', { name: 'Floor' }),
  ).toHaveAttribute('aria-current', 'page');
});

test('the top row opens a factory on a phone', async ({ page }) => {
  test.skip(!isPhone(page), 'the top row is the phone navigation');
  await page.goto('/o/acme');
  const sre = orgNav(page).getByRole('link', { name: 'S, sre factory, 3 waiting on you' });
  await sre.click();
  await expect(page).toHaveURL(/\/o\/acme\/f\/sre$/);
  await expect(sre).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('heading', { level: 1, name: 'SRE floor' })).toBeFocused();
  await expect(page.getByText('Acme/sre')).toBeVisible();
});

test('a decision made elsewhere updates the badges without a reload', async ({ page, request }) => {
  await page.goto('/o/acme/f/goal/board');
  await expect(page.getByText(/^Live/).filter({ visible: true }).first()).toBeVisible();
  const goal = orgNav(page).getByRole('link', { name: /^G, goal factory/ });
  await expect(goal).toHaveAccessibleName('G, goal factory, 2 waiting on you, 1 item blocked');
  await expect(orgNav(page).getByRole('link', { name: 'Su, support factory, blocked: andon pulled' })).toBeVisible();
  const items = page.getByRole('navigation', { name: 'goal factory' }).getByRole('link', { name: /^Items/ });
  if (!isPhone(page)) await expect(items).toHaveAccessibleName(/^Items\s?2 waiting on you$/);

  // Someone else approves (the CLI, another tab): the stream brings the change.
  await request.post('/v1/orgs/acme/items/goal%2312/approve');

  await expect(goal).toHaveAccessibleName('G, goal factory, 1 waiting on you, 1 item blocked');
  if (!isPhone(page)) await expect(items).toHaveAccessibleName(/^Items\s?1 waiting on you$/);
});

test('the organization pages have the organization sidebar', async ({ page }) => {
  test.skip(isPhone(page), 'sidebars are the 768 px and wider navigation');
  await page.goto('/o/acme/f/goal');
  await orgNav(page).getByRole('link', { name: 'Ac, Acme organization, factories' }).click();
  await expect(page).toHaveURL(/\/o\/acme$/);
  const sidebar = page.getByRole('navigation', { name: 'Acme organization' });
  await expect(sidebar.getByRole('link', { name: /^Factories\s?3 factories$/ })).toHaveAttribute(
    'aria-current',
    'page',
  );
  await expect(page.getByRole('heading', { level: 1, name: 'Factories' })).toBeFocused();

  await page.getByRole('link', { name: 'Open sre' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'SRE floor' })).toBeVisible();
});

test('approve an item waiting on you from the floor', async ({ page }) => {
  await page.goto('/o/acme/f/goal');
  const needs = page.getByRole('region', { name: 'Needs you' });
  await needs.getByRole('button', { name: 'Approve goal#12' }).click();

  await expect(page.getByText('Approved goal#12. It is now at Build.')).toBeVisible();
  await expect(needs.getByRole('link', { name: /goal#12/ })).toHaveCount(0);

  await page.goto('/o/acme/f/goal/items/12');
  await expect(page.getByText('At Build', { exact: true })).toBeVisible();
  await showTrace(page);
  await expect(page.getByText('Local user approved')).toBeVisible();
});

test('send an item back with a reason', async ({ page }) => {
  await page.goto('/o/acme/f/goal/items/12');
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
  await page.goto('/o/acme/f/goal/items/6');
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
  await page.goto('/o/acme/f/goal/items/5');
  await expect(page.getByText('Blocked, needs a retry').first()).toBeVisible();
  await page.getByRole('button', { name: 'Retry' }).click();

  await expect(page.getByText('goal#5 is back in the queue.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Retry' })).toHaveCount(0);
});

test('the board updates live when an item moves elsewhere', async ({ page, request }) => {
  test.skip(isPhone(page), 'the columns are the desktop board');
  await page.goto('/o/acme/f/goal/board');
  const approveSpec = page.getByRole('region', { name: 'Approve spec', exact: true });
  await expect(approveSpec.getByRole('link', { name: 'goald: serve the embedded web UI' })).toBeVisible();
  await expect(page.getByText('Live', { exact: true }).first()).toBeVisible();

  // Someone else approves (the CLI, another tab): the stream brings the change.
  await request.post('/v1/orgs/acme/items/goal%2312/approve');

  await expect(approveSpec.getByRole('link', { name: 'goald: serve the embedded web UI' })).toHaveCount(0);
  const build = page.getByRole('region', { name: 'Build', exact: true });
  await expect(build.getByRole('link', { name: 'goald: serve the embedded web UI' })).toBeVisible();
});

test('on a phone: floor to board to item and back', async ({ page }) => {
  test.skip(!isPhone(page), 'phone navigation');
  await page.goto('/o/acme/f/goal');
  const tabs = page.getByRole('navigation', { name: 'Factory' });

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

test('on a phone: More opens a sheet that leads to the factories', async ({ page }) => {
  test.skip(!isPhone(page), 'phone navigation');
  await page.goto('/o/acme/f/goal');
  const more = page.getByRole('navigation', { name: 'Factory' }).getByRole('button', { name: 'More' });

  await more.click();
  const sheet = page.getByRole('dialog', { name: 'More' });
  await expect(sheet.getByText('Knowledge')).toBeVisible();
  await expectAccessible(page);
  await page.keyboard.press('Escape');
  await expect(sheet).toBeHidden();
  await expect(more).toBeFocused();

  await more.click();
  await sheet.getByRole('link', { name: 'Factories' }).click();
  await expect(page).toHaveURL(/\/o\/acme$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Factories' })).toBeVisible();
  await expect(page.getByRole('navigation', { name: 'Factory' })).toHaveCount(0);
});
