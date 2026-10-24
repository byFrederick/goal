import { expect, type Page, test } from '@playwright/test';
import { expectAccessible, expectBoardLive, expectNoHorizontalScroll, isPhone, orgMarkOf, orgNav } from './support';

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
    // The definition editor is linked; the wizard arrives later (D13): said, not linked.
    await expect(card('goal').getByRole('link', { name: 'Definition' })).toHaveAttribute(
      'href',
      '/o/acme/f/goal/definition',
    );
    await expect(page.getByRole('button', { name: 'New factory Later' })).toHaveAttribute('aria-disabled', 'true');
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
  await expect(page.getByRole('button', { name: 'New factory Later' })).toHaveAttribute('aria-disabled', 'true');

  await banner.click();
  await expect(page).toHaveURL(/\/o\/acme#waiting$/);
  await expect(page.getByRole('region', { name: 'Waiting on you', exact: true })).toBeInViewport();

  // The whole card is the link: a tap on its stats opens the factory.
  await page.getByRole('article', { name: 'sre', exact: true }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'SRE floor' })).toBeVisible();
});

test('an organization without budgets or factories says so', async ({ page, request }) => {
  await page.goto('/o/example-labs');
  await expect(
    page.getByText(
      isPhone(page)
        ? /^\$\d+\.\d\d spent this week, no budget set$/
        : /^Example Labs’ factories\. Nothing is waiting on you, and \$\d+\.\d\d was spent this week\.$/,
    ),
  ).toBeVisible();
  await expect(page.getByRole('region', { name: 'Waiting on you', exact: true })).toContainText(
    'Nothing is waiting on you.',
  );

  await request.post('/v1/orgs', { data: { name: 'Empty Co', slug: 'empty-co' } });
  await page.goto('/o/empty-co');
  await expect(page.getByRole('heading', { level: 2, name: 'No factories in Empty Co yet' })).toBeVisible();
  await expect(page.getByText('goal factory push <dir>')).toBeVisible();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
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
  const mark = orgMarkOf(page);
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
  await expect(rail.getByRole('button', { name: 'LU, Local user, account' })).toBeFocused();
  await page.keyboard.press('ArrowUp');
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
  await expectBoardLive(page);
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
  // The current organization in the menu opens its Factories home.
  await orgMarkOf(page).click();
  await page.getByRole('menuitemradio', { name: /^Acme/ }).click();
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
    await expect(time.getByText('spec-writer-1 on turn 3 of 80, $0.31 of $4.00')).toBeVisible();
  } else {
    await expect(time.getByText('Expedite target: within 60 minutes')).toBeVisible();
    await expect(time.getByText('3 of 80', { exact: true })).toBeVisible();
    await expect(time.getByText('$0.31 of $4.00', { exact: true })).toBeVisible();
    await expect(time.getByText('spec', { exact: true })).toBeVisible();
  }

  const path = page.getByRole('region', { name: 'Path' });
  await expect(path.locator('[aria-current="step"]')).toContainText('spec-writer-1 working');
  await expect(path.getByRole('listitem').filter({ hasText: 'Inbox' })).toContainText(/Spent \d+ min/);
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
  await expect(page.getByRole('region', { name: 'Done', exact: true })).toContainText('Postmortem filed');
  await expect(page.getByRole('region', { name: 'Expedite lane' })).toContainText(
    'sre#12, incident sev2, investigator-2 on turn 2',
  );
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
  await expect(page.getByRole('region', { name: 'Expedite lane' })).toContainText('Expedite lane1 of 1 in use');

  // A push line names its constraint and has no rope.
  await page.goto('/o/acme/f/goal/board');
  await expect(page.getByRole('region', { name: 'The line' })).toContainText(
    'Build is the constraint; the line moves at its pace.',
  );
  await expect(page.getByText('Not enforced yet')).toHaveCount(0);
});

test('on a phone the board picks a station, the constraint first', async ({ page }) => {
  test.skip(!isPhone(page), 'the station picker is the phone board');
  await page.goto('/o/acme/f/sre/board');

  const picker = page.getByRole('tablist', { name: 'Stations' });
  await expect(picker.getByRole('tab', { name: 'Investigate 3 of 3, 2 ready' })).toHaveAttribute(
    'aria-selected',
    'true',
  );
  await expect(page.getByText('The constraint: busy 94%, queue growing')).toBeVisible();
  await expect(page.getByRole('article').filter({ hasText: 'billing-svc' })).toContainText(
    'sre#8, Incident, 1 hbilling-svc deploy rolled back twiceinvestigator-1, turn 3',
  );
  await expect(page.getByRole('article').filter({ hasText: 'Expedite lane' })).toContainText(
    'Expedite laneCheckout API p95 above 900 ms since 14:05sre#12, investigator-2,',
  );

  await picker.getByRole('tab', { name: 'Done 1 today' }).click();
  await expect(page).toHaveURL(/station=done/);
  await expect(page.getByRole('article').filter({ hasText: 'Redis evictions' })).toContainText('Postmortem filed');
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

test('switch organization from the menu by keyboard', async ({ page }) => {
  test.skip(isPhone(page), 'the menu is the 768 px and wider navigation; phones have the sheet');
  await page.goto('/o/acme/f/sre');
  await expect(page.getByRole('heading', { level: 1, name: 'SRE floor' })).toBeVisible();
  const mark = orgMarkOf(page);
  await mark.focus();

  // Escape closes the menu and returns focus to the mark.
  await page.keyboard.press('Enter');
  const menu = page.getByRole('menu', { name: 'Ac, Acme organization, switch organization' });
  await expect(menu).toBeVisible();
  await expect(mark).toHaveAttribute('aria-expanded', 'true');
  await expect(menu.getByRole('menuitemradio', { name: /^Acme\s?3 factories$/ })).toHaveAttribute(
    'aria-checked',
    'true',
  );
  await expect(menu.getByRole('menuitemradio', { name: /^Example Labs\s?1 factory$/ })).toHaveAttribute(
    'aria-checked',
    'false',
  );
  await expect(menu.getByRole('menuitem', { name: /Credentials/ })).toHaveAttribute('aria-disabled', 'true');
  await expect(menu.getByRole('menuitem', { name: 'Organization settings' })).toHaveAttribute(
    'href',
    '/o/acme/settings',
  );
  await expect(menu.getByRole('menuitem', { name: /Members/ })).toHaveCount(0);
  await expectAccessible(page);
  await page.keyboard.press('Escape');
  await expect(menu).toBeHidden();
  await expect(mark).toBeFocused();

  // Arrow keys and Enter choose another organization: its Factories home, its rail.
  await page.keyboard.press('Enter');
  await expect(menu.getByRole('menuitemradio', { name: /^Acme/ })).toBeFocused();
  await page.keyboard.press('ArrowDown');
  await expect(menu.getByRole('menuitemradio', { name: /^Example Labs/ })).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(page).toHaveURL(/\/o\/example-labs$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Factories' })).toBeFocused();
  await expect(orgNav(page).getByRole('link', { name: /factory/ })).toHaveCount(1);
  await expect(orgMarkOf(page, 'EL, Example Labs organization, switch organization')).toBeVisible();

  // The organization used last is where / returns.
  await page.goto('/');
  await expect(page).toHaveURL(/\/o\/example-labs$/);
});

test('switch organization from the sheet on a phone', async ({ page }) => {
  test.skip(!isPhone(page), 'the sheet is the phone navigation');
  await page.goto('/o/acme/f/sre');
  const mark = orgMarkOf(page);

  await expect(mark).toHaveAttribute('aria-haspopup', 'dialog');
  await mark.click();
  const sheet = page.getByRole('dialog', { name: 'Switch organization' });
  await expect(sheet).toBeVisible();
  const acme = sheet.getByRole('link', { name: /^Acme\s?3 factories/ });
  await expect(acme).toHaveAttribute('aria-current', 'true');
  await expect(acme).toBeFocused();
  await expect(sheet.getByRole('link', { name: /Members/ })).toHaveCount(0);
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);

  // Escape closes the sheet and returns focus to the mark.
  await page.keyboard.press('Escape');
  await expect(sheet).toBeHidden();
  await expect(mark).toBeFocused();

  await mark.click();
  await sheet.getByRole('link', { name: /^Example Labs/ }).click();
  await expect(page).toHaveURL(/\/o\/example-labs$/);
  await expect(sheet).toBeHidden();
  await expect(page.getByRole('heading', { level: 1, name: 'Factories' })).toBeFocused();
  await expect(orgNav(page).getByRole('link', { name: /factory/ })).toHaveCount(1);
});

/** Opens Create organization from the menu, or from the sheet on a phone. */
async function openCreateOrg(page: Page) {
  await orgMarkOf(page).click();
  const create = isPhone(page)
    ? page.getByRole('dialog', { name: 'Switch organization' }).getByRole('button', { name: 'Create organization' })
    : page.getByRole('menuitem', { name: 'Create organization' });
  await create.click();
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
  await expect(orgNav(page).getByRole('link', { name: /factory/ })).toHaveCount(0);
  const newMark = orgMarkOf(page, 'AT, Acme Two organization, switch organization');
  await expect(newMark).toBeVisible();
  await expectNoHorizontalScroll(page);

  // The new organization is in the menu, current.
  await newMark.click();
  const current = isPhone(page)
    ? page.getByRole('dialog', { name: 'Switch organization' }).getByRole('link', { name: /^Acme Two\s?0 factories/ })
    : page.getByRole('menuitemradio', { name: /^Acme Two\s?0 factories$/ });
  // On its own Factories home the sheet's link is the current page.
  await expect(current).toHaveAttribute(
    isPhone(page) ? 'aria-current' : 'aria-checked',
    isPhone(page) ? 'page' : 'true',
  );
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

test('the account menu names the local user', async ({ page }) => {
  await page.goto('/o/acme');
  const account = orgNav(page).getByRole('button', { name: 'LU, Local user, account' });
  await account.click();
  const menu = page.getByRole('menu', { name: 'LU, Local user, account' });
  await expect(menu.getByText('Local user', { exact: true })).toBeVisible();
  await expect(menu.getByText('This install')).toBeVisible();
  await expect(menu.getByRole('menuitem', { name: /Sign out/ })).toHaveCount(0);
  await expectAccessible(page);
  await page.keyboard.press('Escape');
  await expect(menu).toBeHidden();
  await expect(account).toBeFocused();
});

test('the organization menu opens organization settings', async ({ page }) => {
  await page.goto('/o/acme/f/goal');
  await orgMarkOf(page).click();
  const settings = isPhone(page)
    ? page.getByRole('dialog', { name: 'Switch organization' }).getByRole('link', { name: 'Organization settings' })
    : page.getByRole('menuitem', { name: 'Organization settings' });
  await settings.click();
  await expect(page).toHaveURL(/\/o\/acme\/settings$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Organization settings' })).toBeFocused();
});
