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
  ['factories', '/o/acme', /^Good /],
  ['floor', '/o/acme/f/goal', 'Floor'],
  ['board', '/o/acme/f/goal/board', 'Goal board'],
  ['item', '/o/acme/f/goal/items/6', 'Floor: throughput bars per day'],
  ['items', '/o/acme/f/goal/items', 'Goal items'],
  ['another factory', '/o/acme/f/sre', 'Floor'],
] as const) {
  test(`${name} is accessible and fits the screen`, async ({ page }) => {
    await page.goto(path);
    // From 768 px the Board's title is Board; its phone header keeps the factory's name.
    const title = name === 'board' && !isPhone(page) ? 'Board' : heading;
    await expect(page.getByRole('heading', { level: 1, name: title })).toBeVisible();
    await expect(orgNav(page)).toBeVisible();
    await expectAccessible(page);
    await expectNoHorizontalScroll(page);
  });
}

test('the root opens the factories of the first organization', async ({ page }) => {
  await page.goto('/');
  await expect(page).toHaveURL(/\/o\/acme$/);
  await expect(page.getByRole('heading', { level: 1, name: /^Good / })).toBeVisible();
  await expect(page.getByRole('article')).toHaveCount(4);
  await expect(page).toHaveTitle('Home · Acme · Goal');
});

test('the root returns to the organization used last', async ({ page }) => {
  await page.goto('/o/goal');
  await expect(page.getByRole('heading', { level: 1, name: /^Good / })).toBeVisible();
  await page.goto('/');
  await expect(page).toHaveURL(/\/o\/goal$/);
  await expect(page.getByRole('article')).toHaveCount(1);
  await expect(orgNav(page).getByRole('link', { name: /, \S+ factory/ })).toHaveCount(1);
});

test('the factories home shows each factory’s line, what needs you and the week’s spend', async ({ page }) => {
  await page.goto('/o/acme');
  const row = (name: string) =>
    page.getByRole('article').filter({ has: page.getByRole('heading', { name, exact: true }) });
  await expect(row('software').getByText('Running')).toBeVisible();
  await expect(row('sre').getByText('Andon pulled')).toBeVisible();
  await expect(page.getByRole('region', { name: 'sre line' })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Needs you first' })).toBeVisible();
  await expect(
    page.getByRole('region', { name: 'Needs you first' }).getByRole('link', { name: 'Open all' }),
  ).toHaveAttribute('href', '/o/acme/needs-you');

  const spend = page.getByRole('region', { name: 'Spend this week' });
  await expect(spend.getByText('Last 7 days')).toBeVisible();
  // Not built yet: the link is disabled and never navigates.
  await expect(spend.getByRole('button', { name: 'Spend and billing' })).toHaveAttribute('aria-disabled', 'true');
});

test('on a phone: a factory’s name opens its floor, and its line scrolls in its own box', async ({ page }) => {
  test.skip(!isPhone(page), 'the phone layout of the factories home');
  await page.goto('/o/acme');
  await expect(page.getByRole('region', { name: 'sre line' })).toBeVisible();
  await page.getByRole('link', { name: 'sre', exact: true }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Floor' })).toBeVisible();
});

test('an organization without budgets or factories says so', async ({ page, request }) => {
  await page.goto('/o/goal');
  await expect(page.getByRole('region', { name: 'Needs you first' })).toContainText('Nothing is waiting on you.');

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
  await expect(page.getByRole('heading', { level: 1, name: /^Good / })).toBeVisible();

  const sidebar = orgNav(page);
  const sre = sidebar.getByRole('link', { name: 'S, sre factory, 3 waiting on you' });
  await sre.click();
  await expect(page).toHaveURL(/\/o\/acme\/f\/sre$/);
  await expect(sre).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('heading', { level: 1, name: 'Floor' })).toBeFocused();
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
  await expect(page.getByRole('heading', { level: 1, name: 'Floor' })).toBeFocused();
  // The top bar names the page; the factory is in the drawer, not in a crumb.
  await expect(page.getByRole('banner')).toContainText('Floor');
});

test('a decision made elsewhere updates the badges without a reload', async ({ page, request }) => {
  await page.goto('/o/acme/f/goal/board');
  await expectBoardLive(page);
  const goal = orgNav(page).getByRole('link', { name: /^G, goal factory/ });
  await expect(goal).toHaveAccessibleName('G, goal factory, 2 waiting on you, 1 item blocked');
  await expect(orgNav(page).getByRole('link', { name: /^\S+, sre factory, blocked: andon pulled$/ })).toBeVisible();

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
  await expect(page.getByRole('heading', { level: 1, name: /^Good / })).toBeFocused();

  await page.getByRole('link', { name: 'Open sre' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Floor' })).toBeVisible();
});

test('the floor says what waits on you and leads to it', async ({ page }) => {
  await page.goto('/o/acme/f/goal');

  // What waits on you is the Needs you page, filtered to this factory: the decisions and blocked work,
  // each a row that leads to its item.
  await page.getByRole('link', { name: /^Needs you/ }).click();
  await expect(page).toHaveURL(/\/o\/acme\/needs-you\?factory=goal$/);
  await expect(page.getByRole('link', { name: /goal#5 .*Blocked at Build/ })).toBeVisible();
  await expect(page.getByRole('link', { name: /goal#16/ })).toHaveCount(0);
  await expectAccessible(page);

  await page.getByRole('link', { name: /goal#5 .*Blocked at Build/ }).click();
  await expect(page).toHaveURL(/\/o\/acme\/f\/goal\/items\/5$/);
});

test('a stopped line is what its floor needs first, and clears from it', async ({ page, request }) => {
  await page.goto('/o/acme/needs-you?factory=support');
  await expect(
    page.getByRole('link', { name: /^Line stopped: Answers sent back 4 of the last 10 times/ }),
  ).toBeVisible();

  // Someone clears it (the CLI, another tab): the stream brings the change.
  await request.post('/v1/orgs/acme/factories/support/andon/clear', { data: {} });
  await expect(page.getByRole('link', { name: /^Line stopped/ })).toHaveCount(0);
});

test('approve from the phone: the Needs you page decides the oldest decision', async ({ page }) => {
  test.skip(!isPhone(page), 'the approve view is the phone layout of the Needs you page');
  await page.goto('/o/acme/needs-you?factory=sre');

  await page.getByRole('link', { name: /sre#4/ }).click();
  await expect(page).toHaveURL(/\/o\/acme\/needs-you\?.*factory=sre/);
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);

  await page.getByRole('button', { name: 'Approve', exact: true }).click();
  await expectToast(page, 'Approved sre#4.');
});

test('merging from the phone names every pull request the merge lands', async ({ page, request }) => {
  test.skip(!isPhone(page), 'the approve view is the phone layout of the Needs you page');
  // goal#12 is older; once approved, goal#6 (two pull requests, at Merge) is the one to decide.
  await request.post('/v1/orgs/acme/items/goal%2312/approve', { data: {} });
  await page.goto('/o/acme/needs-you?factory=goal');
  await page.getByRole('link', { name: /goal#6/ }).click();
  await page.getByRole('button', { name: 'Merge', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Merge 2 pull requests?' });
  await expect(dialog.getByRole('link', { name: /Review acme\/goal PR #38 first/ })).toBeVisible();
  await expect(dialog.getByRole('link', { name: /Review acme\/design-tokens MR !12 first/ })).toBeVisible();
  await expectAccessible(page);
});

test('an item shows its stepper: each station with its time, and the step it is at', async ({ page }) => {
  // goal#16: an expedite fix created 38 minutes ago, its spec being written now.
  await page.goto('/o/acme/f/goal/items/16');
  const steps = page.getByRole('list', { name: 'Progress through the line' });
  await expect(steps.locator('[aria-current="step"]')).toContainText('Spec');
  await expect(steps.getByRole('listitem').first()).toContainText(/\d+ min/);
  // The done station says the lead time: the expedite target and the time so far.
  await expect(steps.getByRole('listitem').last()).toContainText(/Target 60 min, \d+ min so far/);
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
    page.getByRole('region', { name: 'Timeline' }).getByText('Cover the empty-bucket case with a test.'),
  ).toBeVisible();
  // The send-back is a person's touch: the Human touches card says it with its reason too.
  await expect(
    page.getByRole('region', { name: 'Human touches' }).getByText(/sent it back: “Cover the empty-bucket case/),
  ).toBeVisible();
});

test('cancel an open item with a reason: it stops, and Cancel item goes', async ({ page }) => {
  await page.goto('/o/acme/f/goal/items/12');
  await page.getByRole('button', { name: 'Cancel item' }).click();

  const dialog = page.getByRole('dialog', { name: 'Cancel goal#12?' });
  await dialog.getByLabel('Reason').fill('Duplicate of #3');
  await expectAccessible(page);
  await dialog.getByRole('button', { name: 'Cancel item' }).click();

  await expect(dialog).toBeHidden();
  await expectToast(page, 'Cancelled goal#12.');
  await expect(page.getByRole('button', { name: 'Cancel item' })).toBeHidden();
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

test('each run card names its station, role, outcome, turns and cost', async ({ page }) => {
  await page.goto('/o/acme/f/goal/items/8');
  await expect(page.getByRole('heading', { level: 1, name: 'Worker: report usage per message' })).toBeVisible();
  await showTrace(page);
  const trace = page.getByRole('region', { name: 'Timeline' });

  const spec = trace.getByRole('article', { name: 'Spec writer' });
  await expect(spec.getByText('Spec', { exact: true })).toBeVisible();
  await expect(spec.getByText('Finished', { exact: true })).toBeVisible();
  await expect(spec.getByText('6 turns')).toBeVisible();
  await expect(spec.getByText('$0.24')).toBeVisible();

  const build = trace.getByRole('article', { name: 'Builder' });
  await expect(build.getByText('Build', { exact: true })).toBeVisible();
  await expect(build.getByText('Running', { exact: true })).toBeVisible();
  await expect(build.getByText('22 turns')).toBeVisible();
  await expect(build.getByText('$1.12')).toBeVisible();
  await expect(build.getByRole('list', { name: 'Events by Builder' })).toBeVisible();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('leave a note for the agent: it will be in the next run', async ({ page }) => {
  await page.goto('/o/acme/f/goal/items/8');
  await expect(page.getByRole('heading', { level: 1, name: 'Worker: report usage per message' })).toBeVisible();

  // One composer per layout: in the trace from 720 px, at the end of the page on a phone.
  const field = page.getByLabel('Note for the agent').filter({ visible: true });
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
  const trace = page.getByRole('region', { name: 'Timeline' });
  await expect(trace.getByText('Local user left a note for the next run')).toBeVisible();
  await expect(trace.getByText('Also report the cache tokens.')).toBeVisible();
});

test('a done item takes no notes', async ({ page }) => {
  await page.goto('/o/acme/f/goal/items/4');
  await expect(page.getByText('At Done', { exact: true })).toBeVisible();
  await expect(page.getByLabel(/^Note for the agent/)).toHaveCount(0);
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

  // Each station's column header card: its name, its count and what it says of its load.
  const columns = page.getByRole('region', { name: 'Stations', exact: true });
  await expect(columns.getByRole('region', { name: 'Inbox', exact: true })).toContainText('No limit');
  await expect(columns.getByRole('region', { name: 'Triage', exact: true })).toContainText('1 of 4');
  await expect(columns.getByRole('region', { name: 'Investigate', exact: true })).toContainText('3 of 3');
  await expect(columns.getByRole('region', { name: 'Approve', exact: true })).toContainText('3 of 5');
  await expect(columns.getByRole('region', { name: 'Done', exact: true })).toContainText('Merged pull requests');

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

test('the line shows its work in progress and the expedite lane in use', async ({ page }) => {
  await page.goto('/o/acme/f/sre/board');
  if (isPhone(page)) {
    await expect(page.getByText('Line running, WIP 7 of 12')).toBeVisible();
    return;
  }

  // The desktop board has no line card: the expedite lane is captioned above the columns.
  await expect(page.getByRole('region', { name: 'Stations', exact: true })).toContainText(
    '1 of 1 in use, jumps every queue',
  );
  await expect(page.getByText('Not enforced yet')).toHaveCount(0);
});

test('on a phone the board opens where a person is needed, then picks any station', async ({ page }) => {
  test.skip(!isPhone(page), 'the station picker is the phone board');
  await page.goto('/o/acme/f/sre/board');

  // Items waiting on a person come first: the board opens on Approve, not the constraint.
  const picker = page.getByRole('tablist', { name: 'Stations' });
  await expect(picker.getByRole('tab', { name: /^Approve/ })).toHaveAttribute('aria-selected', 'true');
  // The phone has no status line: the picked station's column names the constraint.
  await picker.getByRole('tab', { name: /^Investigate/ }).click();
  await expect(page).toHaveURL(/station=investigate/);
  await expect(page.getByRole('tabpanel').getByRole('heading', { name: 'Investigate' })).toBeVisible();
  await expect(page.getByRole('tabpanel').getByText('Constraint', { exact: true })).toBeVisible();
  await expect(page.getByRole('article').filter({ hasText: 'billing-svc' })).toContainText('sre#8');
  await expect(page.getByRole('tabpanel').getByRole('article').filter({ hasText: 'Checkout API p95' })).toContainText(
    'Expedite',
  );

  await picker.getByRole('tab', { name: /^Done/ }).click();
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
  await expect(page.getByRole('heading', { level: 1, name: 'Floor' })).toBeVisible();
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
  await expect(page.getByRole('heading', { level: 1, name: /^Good / })).toBeVisible();
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
  await expect(page.getByRole('heading', { level: 1, name: 'Floor' })).toBeVisible();
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
  await expect(page.getByRole('heading', { level: 1, name: /^Good / })).toBeFocused();
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
  await expect(page.getByRole('heading', { level: 1, name: /^Good / })).toBeFocused();
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
  await expect(page.getByRole('heading', { level: 1, name: 'Floor' })).toBeVisible();

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
  await expect(page.getByRole('heading', { level: 1, name: /^Good / })).toBeVisible();
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
