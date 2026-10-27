import { expect, type Page, test } from '@playwright/test';
import {
  expectAccessible,
  expectBoardLive,
  expectNoHorizontalScroll,
  expectToast,
  isPhone,
  openNavigation,
  orgMarkOf,
  orgNav,
  recordToasts,
} from './support';

test.beforeEach(async ({ page, request }) => {
  await request.post('/__mock/reset');
  await recordToasts(page);
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
  await page.goto('/o/goal');
  await expect(page.getByRole('heading', { level: 1, name: 'Factories' })).toBeVisible();
  await page.goto('/');
  await expect(page).toHaveURL(/\/o\/goal$/);
  await expect(page.getByRole('article')).toHaveCount(1);
  await expect(orgNav(page).getByRole('link', { name: /, \S+ factory/ })).toHaveCount(1);
});

test('the factories home shows each factory’s week and what waits on you', async ({ page, request }) => {
  await page.goto('/o/acme');
  const card = (name: string) => page.getByRole('article', { name, exact: true });
  await expect(card('goal').getByText('Running')).toBeVisible();
  await expect(card('support').getByText('Andon pulled')).toBeVisible();

  if (!isPhone(page)) {
    await expect(
      page.getByText(
        /^Acme’s factories\. 5 items are waiting on you, and \$\d+\.\d\d of the \$80 weekly budget is spent\.$/,
      ),
    ).toBeVisible();
    await expect(card('goal').getByRole('meter', { name: /^\$\d+\.\d\d of \$60 weekly budget, \d+%$/ })).toBeVisible();
    await expect(card('goal').getByRole('img', { name: /^Items done per day, .+: (\d+, ){6}\d+$/ })).toBeVisible();
    await expect(card('goal').getByText(/^[+−]\d\.\d vs last week$|^Same as last week$/)).toBeVisible();
    await expect(card('support').getByText('No budget set')).toBeVisible();
    // The definition editor and the New factory wizard are linked.
    await expect(card('goal').getByRole('link', { name: 'Definition' })).toHaveAttribute(
      'href',
      '/o/acme/f/goal/definition',
    );
    await expect(page.getByRole('main').getByRole('link', { name: 'Import YAML' }).first()).toHaveAttribute(
      'href',
      '/o/acme/new/start?from=yaml',
    );
  }

  const waiting = page.getByRole('region', { name: 'Waiting on you', exact: true });
  await expect(waiting.getByRole('listitem')).toHaveCount(5);
  const newest = waiting.getByRole('link', { name: /^Open \S+#\d+$/ });
  const ref = (await newest.textContent())?.replace('Open ', '') ?? '';
  await expect(waiting.getByRole('listitem').first()).toContainText(ref);

  // Someone else approves (the CLI, another tab): the stream brings the change.
  await request.post('/v1/orgs/acme/items/goal%2312/approve');
  await expect(waiting.getByRole('listitem')).toHaveCount(4);

  await waiting.getByRole('listitem').filter({ hasText: 'goal#6' }).getByRole('link').click();
  await expect(page).toHaveURL(/\/o\/acme\/f\/goal\/items\/6$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Floor: throughput bars per day' })).toBeVisible();
});

test('on a phone: the banner leads to what waits, a card to its floor', async ({ page }) => {
  test.skip(!isPhone(page), 'the phone layout of the factories home');
  await page.goto('/o/acme');
  const banner = page.getByRole('link', { name: /^5 waiting on you/ });
  await expect(banner).toContainText('goal 2, sre 3');
  await expect(page.getByText(/^\$\d+\.\d\d of Acme’s \$80 weekly budget spent$/)).toBeVisible();
  await expect(page.getByRole('main').getByRole('link', { name: 'New factory' })).toHaveAttribute(
    'href',
    '/o/acme/new/start',
  );

  await banner.click();
  await expect(page).toHaveURL(/\/o\/acme#waiting$/);
  await expect(page.getByRole('region', { name: 'Waiting on you', exact: true })).toBeInViewport();

  // The whole card is the link: a tap on its stats opens the factory.
  await page.getByRole('article', { name: 'sre', exact: true }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'SRE floor' })).toBeVisible();
});

test('an organization without budgets or factories says so', async ({ page, request }) => {
  await page.goto('/o/goal');
  await expect(
    page.getByText(
      isPhone(page)
        ? /^\$\d+\.\d\d spent this week, no budget set$/
        : /^Goal’s factories\. Nothing is waiting on you, and \$\d+\.\d\d was spent this week\.$/,
    ),
  ).toBeVisible();
  await expect(page.getByRole('region', { name: 'Waiting on you', exact: true })).toContainText(
    'Nothing is waiting on you.',
  );

  await request.post('/v1/orgs', { data: { name: 'Empty Co', slug: 'empty-co' } });
  await page.goto('/o/empty-co');
  await expect(page.getByRole('heading', { level: 2, name: 'No factories in Empty Co yet' })).toBeVisible();
  await expect(page.getByRole('main').getByRole('link', { name: 'New factory' })).toHaveAttribute(
    'href',
    '/o/empty-co/new/start',
  );
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('old /f/ links move under their organization, keeping search params', async ({ page }) => {
  await page.goto('/f/goal/items/12');
  await expect(page).toHaveURL(/\/o\/acme\/f\/goal\/items\/12$/);
  await expect(page.getByRole('heading', { level: 1, name: 'goald: serve the embedded web UI' })).toBeVisible();

  await page.goto('/f/research/items?state=done');
  await expect(page).toHaveURL(/\/o\/goal\/f\/research\/items\?state=done$/);

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

test('the sidebar opens a factory and marks its pages', async ({ page }) => {
  test.skip(isPhone(page), 'the sidebar is the 768 px and wider navigation');
  await page.goto('/o/acme');
  await expect(page.getByRole('heading', { level: 1, name: 'Factories' })).toBeVisible();

  const sidebar = orgNav(page);
  const sre = sidebar.getByRole('link', { name: 'S, sre factory, 3 waiting on you' });
  await sre.click();
  await expect(page).toHaveURL(/\/o\/acme\/f\/sre$/);
  await expect(sre).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('heading', { level: 1, name: 'SRE floor' })).toBeFocused();
  await expect(sidebar.getByRole('link', { name: 'Floor' })).toHaveAttribute('aria-current', 'page');
  // Agents has no page yet: listed, disabled, and it says so.
  await expect(sidebar.getByRole('button', { name: 'Agents' })).toHaveAttribute('aria-disabled', 'true');
  // A connection's test failed (pagerduty): the sidebar's Connections says so.
  await expect(sidebar.getByRole('link', { name: 'Connections, 1 failing' })).toBeVisible();
});

test('the sidebar collapses to a rail and remembers it', async ({ page }) => {
  test.skip(isPhone(page), 'the sidebar and its rail are the 768 px and wider navigation');
  await page.goto('/o/acme');
  const sidebar = orgNav(page);
  await sidebar.getByRole('button', { name: 'Collapse sidebar' }).click();
  await expect(sidebar.getByRole('button', { name: 'Expand sidebar' })).toBeFocused();
  await expect(sidebar.getByRole('link', { name: 'Home' })).toBeVisible();
  await expect(sidebar.getByRole('link', { name: /^Needs you/ })).toHaveAttribute('href', '/o/acme/needs-you');

  // The choice is kept across a reload: the rail shows its icons and no spend text.
  await page.reload();
  await expect(sidebar.getByRole('button', { name: 'Expand sidebar' })).toBeVisible();
  await expect(sidebar.getByText('Spend this week')).toHaveCount(0);

  await sidebar.getByRole('button', { name: 'Expand sidebar' }).click();
  await expect(sidebar.getByRole('button', { name: 'Collapse sidebar' })).toBeFocused();
  await expect(sidebar.getByText('Spend this week')).toBeVisible();
});

test('the drawer opens a factory on a phone', async ({ page }) => {
  test.skip(!isPhone(page), 'the drawer is the phone navigation');
  await page.goto('/o/acme');
  await openNavigation(page);
  const sre = orgNav(page).getByRole('link', { name: 'S, sre factory, 3 waiting on you' });
  await sre.click();
  await expect(page).toHaveURL(/\/o\/acme\/f\/sre$/);
  await expect(page.getByRole('dialog', { name: 'Acme' })).toBeHidden();
  await expect(page.getByRole('heading', { level: 1, name: 'SRE floor' })).toBeFocused();
  // The top bar names the page; the factory is in the drawer, not in a crumb.
  await expect(page.getByRole('banner')).toContainText('Floor');
});

test('a decision made elsewhere updates the badges without a reload', async ({ page, request }) => {
  await page.goto('/o/acme/f/goal/board');
  await expectBoardLive(page);
  const goal = orgNav(page).getByRole('link', { name: /^G, goal factory/ });
  await expect(goal).toHaveAccessibleName('G, goal factory, 2 waiting on you, 1 item blocked');
  await expect(orgNav(page).getByRole('link', { name: 'Su, support factory, blocked: andon pulled' })).toBeVisible();

  // Someone else approves (the CLI, another tab): the stream brings the change.
  await request.post('/v1/orgs/acme/items/goal%2312/approve');

  await expect(goal).toHaveAccessibleName('G, goal factory, 1 waiting on you, 1 item blocked');
});

test('the sidebar marks Home on the organization pages', async ({ page }) => {
  test.skip(isPhone(page), 'sidebars are the 768 px and wider navigation');
  await page.goto('/o/acme/f/goal');
  // The current organization in the menu opens its Factories home.
  await orgMarkOf(page).click();
  await page.getByRole('menuitem', { name: /^Acme/ }).click();
  await expect(page).toHaveURL(/\/o\/acme$/);
  const sidebar = orgNav(page);
  await expect(sidebar.getByRole('link', { name: 'Home' })).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('heading', { level: 1, name: 'Factories' })).toBeFocused();

  await page.getByRole('link', { name: 'Open sre' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'SRE floor' })).toBeVisible();
});

test('the floor says what waits on you and leads to it', async ({ page }) => {
  await page.goto('/o/acme/f/goal');

  // What waits on you is the Inbox: the decisions, then blocked work, each a row that leads to its item.
  await page.getByRole('button', { name: /^Inbox/ }).click();
  const drawer = page.getByRole('dialog', { name: 'Inbox' });
  await expect(drawer.getByRole('heading', { name: 'Needs a decision' })).toBeVisible();
  await expect(drawer.getByRole('link', { name: /goal#5 .*Blocked at Build/ })).toBeVisible();
  await expect(drawer.getByRole('link', { name: /goal#16/ })).toHaveCount(0);
  await expectAccessible(page);

  await drawer.getByRole('link', { name: /goal#5 .*Blocked at Build/ }).click();
  await expect(page).toHaveURL(/\/o\/acme\/f\/goal\/items\/5$/);
});

test('a stopped line is what its floor needs first, and clears from it', async ({ page, request }) => {
  await page.goto('/o/acme/f/support');
  await page.getByRole('button', { name: /^Inbox/ }).click();
  const drawer = page.getByRole('dialog', { name: 'Inbox' });
  await expect(
    drawer.getByRole('link', { name: /^Line stopped: Answers sent back 4 of the last 10 times/ }),
  ).toBeVisible();

  // Someone clears it (the CLI, another tab): the stream brings the change.
  await request.post('/v1/orgs/acme/factories/support/andon/clear', { data: {} });
  await expect(drawer.getByRole('link', { name: /^Line stopped/ })).toHaveCount(0);
});

test('approve from the phone: the Inbox drawer decides the oldest decision', async ({ page }) => {
  test.skip(!isPhone(page), 'the phone Items view is the one that opens the Inbox drawer');
  await page.goto('/o/acme/f/sre/items');
  await expect(page.getByRole('heading', { name: 'Needs you' })).toHaveCount(0);

  await page.getByRole('button', { name: /^Inbox/ }).click();
  const drawer = page.getByRole('dialog', { name: 'Inbox' });
  await expect(drawer.getByRole('link', { name: /sre#4/ })).toBeVisible();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);

  await drawer.getByRole('button', { name: 'Approve sre#4' }).click();
  await expectToast(page, 'Approved sre#4.');
  await expect(drawer.getByRole('button', { name: 'Approve sre#4' })).toHaveCount(0);
});

test('merging from the phone names every pull request the merge lands', async ({ page, request }) => {
  test.skip(!isPhone(page), 'the phone Items view is the one that opens the Inbox drawer');
  // goal#12 is older; once approved, goal#6 (two pull requests, at Merge) is the one to decide.
  await request.post('/v1/orgs/acme/items/goal%2312/approve', { data: {} });
  await page.goto('/o/acme/f/goal/items');
  await page.getByRole('button', { name: /^Inbox/ }).click();
  const drawer = page.getByRole('dialog', { name: 'Inbox' });
  await drawer.getByRole('button', { name: 'Merge goal#6' }).click();
  const dialog = page.getByRole('dialog', { name: 'Merge 2 pull requests?' });
  await expect(dialog.getByRole('link', { name: /Review acme\/goal PR #38 first/ })).toBeVisible();
  await expect(dialog.getByRole('link', { name: /Review acme\/design-tokens MR !12 first/ })).toBeVisible();
  await expectAccessible(page);
});

test('an item shows its time at each station against its target', async ({ page }) => {
  // goal#16: an expedite fix created 38 minutes ago, its spec being written now.
  await page.goto('/o/acme/f/goal/items/16');
  const time = page.getByRole('region', { name: /so far$/ });
  await expect(time.getByRole('heading', { name: /^3[89] min so far$/ })).toBeVisible();
  await expect(
    time.getByRole('img', {
      name: /^Inbox \d+ min, Spec \d+ min \(\d+ min waiting, \d+ min working\), 2[12] min left/,
    }),
  ).toBeVisible();
  await expect(time.getByText(/^Inbox \d+ min$/)).toBeVisible();
  await expect(time.getByText('60 min', { exact: true })).toBeVisible();

  // Below 720 px of content (phones, and tablets beside the sidebar) the card is compact.
  if ((await page.locator('main').evaluate((main) => main.clientWidth)) < 720) {
    await expect(time.getByText('target 60 min')).toBeVisible();
    await expect(time.getByText('spec-writer-1 on turn 3 of 80, $0.31 of $12.00')).toBeVisible();
  } else {
    await expect(time.getByText('Expedite target: within 60 minutes')).toBeVisible();
    await expect(time.getByText('3 of 80', { exact: true })).toBeVisible();
    await expect(time.getByText('$0.31 of $12.00', { exact: true })).toBeVisible();
    await expect(time.getByText('spec', { exact: true })).toBeVisible();
  }

  const path = page.getByRole('region', { name: 'Path' });
  await expect(path.locator('[aria-current="step"]')).toContainText('spec-writer-1 working');
  await expect(path.getByRole('listitem').filter({ hasText: 'Inbox' })).toContainText(/Spent \d+ min/);
  // A station not reached yet is named, never given a state it does not have ("Finished").
  await expect(path.getByRole('listitem').last()).toHaveText('Done');
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
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
  await expectToast(page, 'Sent goal#12 back. It is now at Spec.');
  await expect(page.getByText('At Spec', { exact: true })).toBeVisible();
  await showTrace(page);
  await expect(
    page.getByRole('region', { name: 'Trace' }).getByText('Cover the empty-bucket case with a test.'),
  ).toBeVisible();
  // The send-back is a person's touch: the Human touches card says it with its reason too.
  await expect(
    page.getByRole('region', { name: 'Human touches' }).getByText(/sent it back: “Cover the empty-bucket case/),
  ).toBeVisible();
});

test('merge every pull request at the merge station', async ({ page }) => {
  await page.goto('/o/acme/f/goal/items/6');
  // goal#6 opened two: one on GitHub, one on GitLab; the item and its output name both.
  const outputs = page.getByRole('region', { name: 'Outputs' });
  await expect(outputs.getByRole('link', { name: /acme\/goal PR #38/ })).toBeVisible();
  await expect(outputs.getByRole('link', { name: /acme\/design-tokens MR !12/ })).toHaveAttribute(
    'href',
    'https://gitlab.example.com/acme/design-tokens/-/merge_requests/12',
  );
  await page.getByRole('button', { name: 'Merge' }).click();

  const dialog = page.getByRole('dialog', { name: 'Merge 2 pull requests?' });
  await expect(dialog.getByRole('link', { name: /Review acme\/goal PR #38 first/ })).toHaveAttribute(
    'href',
    'https://github.com/acme/goal/pull/38',
  );
  await expect(dialog.getByRole('link', { name: /Review acme\/design-tokens MR !12 first/ })).toBeVisible();
  await expectAccessible(page);
  await dialog.getByRole('button', { name: 'Squash and merge' }).click();

  await expect(page.getByText('Merged goal#6. It is done.')).toBeVisible();
  await expect(page.getByText('At Done', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Merge' })).toHaveCount(0);
});

test('a refused merge says which pull request stopped it, and the item stays', async ({ page, request }) => {
  const refusals = [
    {
      code: 'cannot-merge',
      detail: 'https://gitlab.example.com/acme/design-tokens/-/merge_requests/12 has conflicts',
      says: /goald stopped at a pull request it could not merge: .*merge_requests\/12 has conflicts\. Pull requests merged before it stay merged/,
    },
    {
      code: 'no-code-host-connection',
      detail: 'no connection of goal signs git in to gitlab.example.com',
      says: /No connection of this factory signs in to that host, so nothing was merged: no connection of goal signs git in to gitlab\.example\.com/,
    },
  ];
  await page.goto('/o/acme/f/goal/items/6');
  for (const r of refusals) {
    await request.post('/__mock/fail', {
      data: { path: '/items/', method: 'POST', status: 409, code: r.code, detail: r.detail },
    });
    await page.getByRole('button', { name: 'Merge', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Merge 2 pull requests?' });
    await dialog.getByRole('button', { name: /Squash and merge|Merge again/ }).click();
    await expect(dialog.getByRole('alert')).toHaveText(r.says);
    await expect(dialog.getByRole('button', { name: 'Merge again' })).toBeVisible();
    await expectAccessible(page);
    await dialog.getByRole('button', { name: 'Cancel' }).click();
  }
  await expect(page.getByRole('button', { name: 'Merge', exact: true })).toBeVisible();
});

test('retry a blocked item', async ({ page }) => {
  await page.goto('/o/acme/f/goal/items/5');
  await expect(page.getByText('Blocked, needs a retry').first()).toBeVisible();
  await page.getByRole('button', { name: 'Retry' }).click();

  await expect(page.getByText('goal#5 is back in the queue.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Retry' })).toHaveCount(0);
});

test('the trace heads each run with its agent copy, model, turns, cost and version', async ({ page }) => {
  await page.goto('/o/acme/f/goal/items/8');
  await expect(page.getByRole('heading', { level: 1, name: 'Worker: report usage per message' })).toBeVisible();
  await showTrace(page);
  const trace = page.getByRole('region', { name: 'Trace' });

  const spec = trace.getByRole('listitem').filter({ hasText: 'spec-writer-1 worked at Spec' });
  await expect(spec.getByText('Finished', { exact: true })).toBeVisible();
  await expect(spec.getByText(/^claude-sonnet-5-5, 6 turns, \$0\.24, .+, ran on v9$/)).toBeVisible();

  const build = trace.getByRole('listitem').filter({ hasText: 'builder-2 worked at Build' });
  await expect(build.getByText('Running', { exact: true })).toBeVisible();
  await expect(build.getByText(/ran on v9$/)).toBeVisible();
  await expect(build.getByRole('list', { name: 'Tool calls by builder-2' })).toBeVisible();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('leave a note for the agent: it will be in the next run', async ({ page }) => {
  await page.goto('/o/acme/f/goal/items/8');
  await expect(page.getByRole('heading', { level: 1, name: 'Worker: report usage per message' })).toBeVisible();

  // One composer per layout: in the trace from 720 px, at the end of the page on a phone.
  const field = page.getByLabel('Note to the builder').filter({ visible: true });
  await expect(field).toHaveCount(1);
  await field.fill('Also report the cache tokens.');
  await page
    .getByRole('button', { name: /^Send( note)?$/ })
    .filter({ visible: true })
    .click();

  await expect(page.getByText('Will be in the next run')).toBeVisible();
  await expect(field).toHaveValue('');
  await expectAccessible(page);

  await showTrace(page);
  const trace = page.getByRole('region', { name: 'Trace' });
  await expect(trace.getByText('Local user left a note for the next run')).toBeVisible();
  await expect(trace.getByText('Also report the cache tokens.')).toBeVisible();
});

test('a done item takes no notes', async ({ page }) => {
  await page.goto('/o/acme/f/goal/items/4');
  await expect(page.getByText('At Done', { exact: true })).toBeVisible();
  await expect(page.getByLabel(/^Note to the/)).toHaveCount(0);
});

test('the board updates live when an item moves elsewhere', async ({ page, request }) => {
  test.skip(isPhone(page), 'the columns are the desktop board');
  await page.goto('/o/acme/f/goal/board');
  const approveSpec = page.getByRole('region', { name: 'Approve spec', exact: true });
  await expect(approveSpec.getByRole('link', { name: 'goald: serve the embedded web UI' })).toBeVisible();
  await expectBoardLive(page);

  // Someone else approves (the CLI, another tab): the stream brings the change.
  await request.post('/v1/orgs/acme/items/goal%2312/approve');

  await expect(approveSpec.getByRole('link', { name: 'goald: serve the embedded web UI' })).toHaveCount(0);
  const build = page.getByRole('region', { name: 'Build', exact: true });
  await expect(build.getByRole('link', { name: 'goald: serve the embedded web UI' })).toBeVisible();
});

test('the board shows how each station is doing and who works each card', async ({ page }) => {
  test.skip(isPhone(page), 'the line and the columns are the desktop board');
  await page.goto('/o/acme/f/sre/board');

  const line = page.getByRole('region', { name: 'Stations in line order' });
  await expect(line.getByRole('listitem').filter({ hasText: 'Inbox' })).toContainText(/3 waiting\s*Oldest 2 h/);
  await expect(line.getByRole('listitem').filter({ hasText: 'Triage' })).toContainText('1 of 4Idle 62% of the time');
  await expect(line.getByRole('listitem').filter({ hasText: 'Investigate' })).toContainText(
    '3 of 3, 2 readyBusy 94%, queue growing',
  );
  await expect(line.getByRole('listitem').filter({ hasText: 'Approve' })).toContainText('3 of 5Waiting on you');
  await expect(line.getByRole('listitem').filter({ hasText: 'Done' })).toContainText('1 today');

  const investigate = page.getByRole('region', { name: 'Investigate', exact: true });
  await expect(investigate.getByRole('article').filter({ hasText: 'billing-svc' })).toContainText(
    'investigator-1, turn 3',
  );
  await expect(investigate.getByRole('article').filter({ hasText: 'Upgrade EKS' })).toContainText('Ready in buffer');
  await expect(page.getByRole('region', { name: 'Triage', exact: true })).toContainText('triager, turn 1');
  await expect(page.getByRole('region', { name: 'Approve', exact: true }).getByText('Needs your OK')).toHaveCount(3);
  await expect(page.getByRole('region', { name: 'Done', exact: true })).toContainText('1 today');
  await expect(page.getByRole('region', { name: 'Done', exact: true })).not.toContainText('Postmortem filed');
  await expect(investigate.getByRole('article').filter({ hasText: 'Checkout API p95' })).toContainText('Expedite');
  await expectAccessible(page);
});

test('the line shows its rope, not enforced yet, its work in progress and the expedite lane in use', async ({
  page,
}) => {
  await page.goto('/o/acme/f/sre/board');
  if (isPhone(page)) {
    await expect(page.getByText('Line running, WIP 7 of 12')).toBeVisible();
    return;
  }

  const line = page.getByRole('region', { name: 'The line' });
  await expect(line).toContainText('New work enters only when Investigate’s buffer drops below 2 Not enforced yet');
  await expect(line).toContainText('WIP 7 of 12');
  await expect(line).toContainText('Expedite 1 of 1 in use');

  // A push line names its constraint and has no rope.
  await page.goto('/o/acme/f/goal/board');
  await expect(page.getByRole('region', { name: 'The line' })).toContainText(
    'Build is the constraint; the line moves at its pace.',
  );
  await expect(page.getByText('Not enforced yet')).toHaveCount(0);
});

test('on a phone the board opens where a person is needed, then picks any station', async ({ page }) => {
  test.skip(!isPhone(page), 'the station picker is the phone board');
  await page.goto('/o/acme/f/sre/board');

  // Items waiting on a person come first: the board opens on Approve, not the constraint.
  const picker = page.getByRole('tablist', { name: 'Stations' });
  await expect(picker.getByRole('tab', { name: /^Approve/ })).toHaveAttribute('aria-selected', 'true');
  // The line's WIP stays one phrase on one line ("WIP 1 / of 2" read badly).
  const wip = page.getByText(/^WIP \d+( of \d+)?$/).filter({ visible: true });
  expect(await wip.evaluate((el) => el.getClientRects().length)).toBe(1);
  await picker.getByRole('tab', { name: 'Investigate 3 of 3, 2 ready' }).click();
  await expect(page).toHaveURL(/station=investigate/);
  await expect(page.getByText('Investigate is the constraint: 3 of 3 busy, 2 waiting')).toBeVisible();
  await expect(page.getByRole('article').filter({ hasText: 'billing-svc' })).toContainText(
    'sre#8, Incident, 1 hbilling-svc deploy rolled back twiceinvestigator-1, turn 3',
  );
  await expect(page.getByRole('tabpanel').getByRole('article').filter({ hasText: 'Checkout API p95' })).toContainText(
    'Expedite',
  );

  await picker.getByRole('tab', { name: 'Done 1 today' }).click();
  await expect(page).toHaveURL(/station=done/);
  await expect(page.getByRole('article').filter({ hasText: 'Redis evictions' })).not.toContainText('Postmortem filed');
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('pull the andon cord, then clear it: the line stops and runs again', async ({ page }) => {
  await page.goto('/o/acme/f/goal/board');
  const status = page.getByRole('status').filter({ hasText: /^Line / });
  const goalSquare = orgNav(page)
    .filter({ visible: true })
    .getByRole('link', { name: /goal factory/ });
  await expect(status).toHaveText(/^Line running/);
  await expect(goalSquare).not.toHaveAccessibleName(/andon pulled/);

  await page.getByRole('button', { name: 'Pull the andon cord' }).click();
  const pull = page.getByRole('dialog', { name: 'Pull the andon cord' });
  await pull.getByRole('button', { name: 'Stop the line' }).click();
  await expect(pull.getByText('Write why, so whoever clears it knows what to fix.')).toBeVisible();
  await pull.getByLabel('Why the line must stop').fill('Builds pass but deploys fail');
  await expectAccessible(page);
  await pull.getByRole('button', { name: 'Stop the line' }).click();

  await expect(pull).toBeHidden();
  await expect(status).toHaveText(/^Line stopped: Builds pass but deploys fail/);
  await expect(goalSquare).toHaveAccessibleName(/blocked: andon pulled/);
  await expectNoHorizontalScroll(page);

  await page.getByRole('button', { name: 'Clear the andon' }).click();
  const clear = page.getByRole('dialog', { name: 'Clear the andon' });
  await expect(clear.getByText(/It was stopped because: Builds pass but deploys fail/)).toBeVisible();
  await clear.getByLabel('What was done about it (optional)').fill('Rotated the deploy key');
  await clear.getByRole('button', { name: 'Restart the line' }).click();

  await expect(clear).toBeHidden();
  await expect(status).toHaveText(/^Line running/);
  await expect(goalSquare).not.toHaveAccessibleName(/andon pulled/);
  await expect(page.getByRole('button', { name: 'Pull the andon cord' })).toBeVisible();
});

test('on a phone: floor to board to item and back', async ({ page }) => {
  test.skip(!isPhone(page), 'phone navigation');
  await page.goto('/o/acme/f/goal');
  const tabs = page.getByRole('navigation', { name: 'Tabs' });

  await tabs.getByRole('link', { name: 'Board' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Goal board' })).toBeVisible();
  await expect(page.getByRole('tab', { name: /^Approve spec/ })).toHaveAttribute('aria-selected', 'true');

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

test('on a phone: More opens the drawer, which leads to the organization pages', async ({ page }) => {
  test.skip(!isPhone(page), 'phone navigation');
  await page.goto('/o/acme/f/goal');
  const more = page.getByRole('navigation', { name: 'Tabs' }).getByRole('button', { name: 'More' });

  await more.click();
  const drawer = page.getByRole('dialog', { name: 'Acme' });
  await expect(drawer.getByRole('link', { name: 'Knowledge' })).toHaveAttribute('href', '/o/acme/knowledge');
  await expectAccessible(page);
  await page.keyboard.press('Escape');
  await expect(drawer).toBeHidden();
  await expect(page.getByRole('button', { name: 'Open menu' })).toBeFocused();

  await more.click();
  await drawer.getByRole('link', { name: 'Home' }).click();
  await expect(page).toHaveURL(/\/o\/acme$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Factories' })).toBeVisible();
  await expect(drawer).toBeHidden();
  // The tab bar stays on the organization's pages; with no factory open it follows the last one visited.
  await expect(page.getByRole('navigation', { name: 'Tabs' }).getByRole('link', { name: 'Board' })).toHaveAttribute(
    'href',
    '/o/acme/f/goal/board',
  );
});

test('switch organization from the menu by keyboard', async ({ page }) => {
  test.skip(isPhone(page), 'the menu is the 768 px and wider navigation; phones open it from the drawer');
  await page.goto('/o/acme/f/sre');
  await expect(page.getByRole('heading', { level: 1, name: 'SRE floor' })).toBeVisible();
  const mark = orgMarkOf(page);
  await mark.focus();

  // Escape closes the menu and returns focus to the mark.
  await page.keyboard.press('Enter');
  const menu = page.getByRole('menu', { name: 'Acme organization, switch organization' });
  await expect(menu).toBeVisible();
  await expect(mark).toHaveAttribute('aria-expanded', 'true');
  await expect(menu.getByRole('menuitem', { name: /^Acme\s?3 factories, owner Current$/ })).toHaveAttribute(
    'aria-current',
    'true',
  );
  await expect(menu.getByRole('menuitem', { name: /^Goal\s?1 factory, owner$/ })).not.toHaveAttribute('aria-current');
  await expect(menu.getByRole('menuitem', { name: 'Spend and billing' })).toHaveAttribute('aria-disabled', 'true');
  await expect(menu.getByRole('menuitem', { name: 'Models and policies' })).toHaveAttribute('aria-disabled', 'true');
  await expect(menu.getByRole('menuitem', { name: 'Organization settings' })).toHaveAttribute(
    'href',
    '/o/acme/settings',
  );
  await expect(menu.getByRole('menuitem', { name: /^Members and roles/ })).toHaveAttribute('href', '/o/acme/members');
  await expectAccessible(page);
  await page.keyboard.press('Escape');
  await expect(menu).toBeHidden();
  await expect(mark).toBeFocused();

  // Arrow keys and Enter choose another organization: its Factories home, its sidebar.
  await page.keyboard.press('Enter');
  await expect(menu.getByRole('menuitem', { name: /^Acme/ })).toBeFocused();
  await page.keyboard.press('ArrowDown');
  await expect(menu.getByRole('menuitem', { name: /^Goal/ })).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(page).toHaveURL(/\/o\/goal$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Factories' })).toBeFocused();
  await expect(orgNav(page).getByRole('link', { name: /, \S+ factory/ })).toHaveCount(1);
  await expect(orgMarkOf(page, 'Goal')).toBeVisible();

  // The organization used last is where / returns.
  await page.goto('/');
  await expect(page).toHaveURL(/\/o\/goal$/);
});

test('switch organization from the drawer on a phone', async ({ page }) => {
  test.skip(!isPhone(page), 'the drawer is the phone navigation');
  await page.goto('/o/acme/f/sre');
  await openNavigation(page);
  await orgMarkOf(page).click();
  const menu = page.getByRole('menu', { name: 'Acme organization, switch organization' });
  await expect(menu.getByRole('menuitem', { name: /^Acme\s?3 factories/ })).toHaveAttribute('aria-current', 'true');
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);

  await menu.getByRole('menuitem', { name: /^Goal/ }).click();
  await expect(page).toHaveURL(/\/o\/goal$/);
  await expect(page.getByRole('dialog', { name: 'Acme' })).toBeHidden();
  await expect(page.getByRole('heading', { level: 1, name: 'Factories' })).toBeFocused();
  await expect(orgNav(page).getByRole('link', { name: /, \S+ factory/ })).toHaveCount(1);
});

/** Opens Create organization from the organization menu (on a phone, in the drawer). */
async function openCreateOrg(page: Page) {
  await openNavigation(page);
  await orgMarkOf(page).click();
  await page.getByRole('menuitem', { name: 'Create organization' }).click();
  const dialog = page.getByRole('dialog', { name: 'Create organization' });
  await expect(dialog).toBeVisible();
  return dialog;
}

test('create an organization and land on its empty factories', async ({ page }) => {
  await page.goto('/o/acme/f/goal');
  await expect(page.getByRole('heading', { level: 1, name: 'Goal floor' })).toBeVisible();

  const dialog = await openCreateOrg(page);
  const name = dialog.getByRole('textbox', { name: 'Name' });
  const address = dialog.getByRole('textbox', { name: 'Address' });
  await expect(name).toBeFocused();
  await expectAccessible(page);

  // The address follows the name; a taken one is refused and named.
  await name.fill('Acme');
  await expect(address).toHaveValue('acme');
  await dialog.getByRole('button', { name: 'Create organization' }).click();
  await expect(dialog.getByText('acme is taken. Choose another address.')).toBeVisible();
  await expect(address).toHaveAttribute('aria-invalid', 'true');
  await expect(address).toBeFocused();

  await name.fill('Acme Two');
  await expect(address).toHaveValue('acme-two');
  await dialog.getByRole('button', { name: 'Create organization' }).click();

  await expect(page).toHaveURL(/\/o\/acme-two$/);
  await expect(dialog).toBeHidden();
  await expect(page.getByRole('heading', { level: 1, name: 'Factories' })).toBeVisible();
  await expect(orgNav(page).getByRole('link', { name: /, \S+ factory/ })).toHaveCount(0);
  const newMark = orgMarkOf(page, 'Acme Two');
  await expect(newMark).toBeVisible();
  await expectNoHorizontalScroll(page);

  // The new organization is in the menu, current.
  await openNavigation(page);
  await newMark.click();
  // The organization menu is used at every width, including in the phone drawer, and its
  // current row is marked with aria-current at every width.
  await expect(page.getByRole('menuitem', { name: /^Acme Two\s?0 factories/ })).toHaveAttribute('aria-current', 'true');
});

test('a malformed address is caught before sending, and Escape returns to the mark', async ({ page }) => {
  await page.goto('/o/acme');
  const dialog = await openCreateOrg(page);
  const name = dialog.getByRole('textbox', { name: 'Name' });
  const address = dialog.getByRole('textbox', { name: 'Address' });

  await dialog.getByRole('button', { name: 'Create organization' }).click();
  await expect(dialog.getByText('Give the organization a name.')).toBeVisible();
  await expect(name).toBeFocused();

  await name.fill('North Star');
  await address.fill('-north');
  await dialog.getByRole('button', { name: 'Create organization' }).click();
  await expect(address).toHaveAccessibleDescription(/^3 to 40 lowercase letters/);
  await address.fill('settings');
  await expect(address).toHaveAccessibleDescription(/^That address is reserved/);
  await expect(address).toBeFocused();

  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  await expect(orgMarkOf(page)).toBeFocused();
  await expect(page).toHaveURL(/\/o\/acme$/);
});

test('with sign-in off the account names the local user and offers nothing to sign out of', async ({ page }) => {
  await page.goto('/o/acme');
  // The account menu at every width; on a phone it opens from the drawer's person row.
  await openNavigation(page);
  const account = orgNav(page).getByRole('button', { name: 'LU, Local user, account' });
  await account.click();
  const panel = page.getByRole('dialog', { name: 'Account' });
  await expect(panel.getByText('Local user', { exact: true })).toBeVisible();
  await expect(panel.getByText('This install')).toBeVisible();
  await expect(
    panel.getByText('Sign-in is off. Anyone who can reach this address acts as the local user.'),
  ).toBeVisible();
  await expect(panel.getByRole('button')).toHaveCount(0);
  await expectAccessible(page);
  await page.keyboard.press('Escape');
  await expect(panel).toBeHidden();
  await expect(account).toBeFocused();
});

test('the organization menu opens organization settings', async ({ page }) => {
  await page.goto('/o/acme/f/goal');
  await openNavigation(page);
  await orgMarkOf(page).click();
  await page.getByRole('menuitem', { name: 'Organization settings' }).click();
  await expect(page).toHaveURL(/\/o\/acme\/settings$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Organization settings' })).toBeFocused();
});
