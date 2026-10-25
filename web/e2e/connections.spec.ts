// Connections (spec 4 §10) against the mock: acme's connections (pagerduty's test fails until its
// key is replaced, status-api misses its secret, gitlab-infra was never tested), New connection,
// a connection's page, and the builder's Basics runtime and Connections and knowledge, where goal
// names github and playwright, claude gives its model and github clones its repository.
import { expect, type Page, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll, expectToast, isPhone, recordToasts } from './support';

test.beforeEach(async ({ page, request }) => {
  await request.post('/__mock/reset');
  await recordToasts(page);
});

const secretValue = 'ghp_e2e-never-shown-again';

async function fits(page: Page) {
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
}

test.describe('the Connections page', () => {
  test('lists each connection with its status, a failed test above the list, and what uses each', async ({ page }) => {
    await page.goto('/o/acme/connections');
    await expect(page).toHaveTitle('Connections · Acme · Goal');
    await expect(page.getByRole('heading', { level: 1, name: 'Connections' })).toBeVisible();
    const failed = page.getByRole('region', { name: 'pagerduty failed its test' });
    await expect(failed).toContainText('answered 401');
    await expect(failed).toContainText('sre’s runs still get PAGERDUTY_API_TOKEN');
    await expect(page.getByRole('heading', { level: 2, name: '8 connections' })).toBeVisible();

    // A table where main is wide, cards where it is not: whichever shows.
    const list = page
      .getByRole('row')
      .or(page.getByRole('list', { name: 'Connections of Acme' }).getByRole('listitem'));
    await expect(list.filter({ hasText: 'pagerduty' })).toContainText('Test failed: 401');
    await expect(list.filter({ hasText: 'status-api' })).toContainText('Missing secret');
    await expect(list.filter({ hasText: 'gitlab-infra' })).toContainText('Never tested');
    if (await page.getByRole('table').isVisible()) {
      await expect(list.filter({ hasText: 'claude' })).toContainText('goalsresupport');
      await expect(list.filter({ hasText: 'claude' })).toContainText('Model access');
      await expect(list.filter({ hasText: 'aws-prod' })).toContainText('Short-lived per run');
    }
    // The secrets themselves are never on the page.
    await expect(page.getByText(/ghp_|sk-ant/)).toHaveCount(0);
    const activity = page.getByRole('region', { name: 'Recent activity' });
    await expect(activity).toContainText('github: token replaced');
    await fits(page);
  });

  test('filters by kind, text and factory in the URL; the factory sidebar opens it filtered', async ({ page }) => {
    await page.goto('/o/acme/connections');
    await page.getByRole('combobox', { name: 'Kind' }).selectOption({ label: 'GitHub' });
    await expect(page).toHaveURL(/kind=github/);
    await expect(page.getByRole('heading', { level: 2, name: '1 of 8 connections' })).toBeVisible();
    await page.getByRole('combobox', { name: 'Kind' }).selectOption({ label: 'All kinds' });
    await page.getByRole('searchbox', { name: 'Find a connection' }).fill('aws');
    await expect(page.getByRole('heading', { level: 2, name: '1 of 8 connections' })).toBeVisible();

    if (isPhone(page)) return; // the factory sidebar is the 768 px and wider navigation
    await page.goto('/o/acme/f/sre');
    await page.getByRole('navigation', { name: 'sre factory' }).getByRole('link', { name: 'Connections' }).click();
    await expect(page).toHaveURL(/\/o\/acme\/connections\?factory=sre$/);
    await expect(page.getByRole('heading', { level: 2, name: /^4 connections sre uses$/ })).toBeVisible();
    await expect(page.getByRole('combobox', { name: 'Factory' })).toHaveValue('sre');
    await fits(page);
  });

  test('a factory the organization has no factory of is said, never the unfiltered list', async ({ page }) => {
    await page.goto('/o/acme/connections?factory=nope');
    await expect(page.getByText('Acme has no factory named nope.')).toBeVisible();
    await expect(page.getByRole('combobox', { name: 'Factory' })).toHaveValue('nope');
    await expect(page.getByRole('heading', { level: 2, name: /8 connections/ })).toHaveCount(0);
    await page.getByRole('button', { name: 'Show every connection' }).click();
    await expect(page.getByRole('heading', { level: 2, name: '8 connections' })).toBeVisible();
    await fits(page);
  });

  test('replacing a revoked key makes its test pass', async ({ page }) => {
    await page.goto('/o/acme/connections');
    await page
      .getByRole('region', { name: 'pagerduty failed its test' })
      .getByRole('button', { name: 'Replace secret…' })
      .click();
    const dialog = page.getByRole('dialog', { name: 'Replace api key of pagerduty' });
    const input = dialog.getByLabel('API key');
    await expect(input).toHaveAttribute('type', 'password');
    // Browsers ignore autocomplete=off on a password input: new-password keeps saved passwords out.
    await expect(input).toHaveAttribute('autocomplete', 'new-password');
    await input.fill('a-fresh-pagerduty-key');
    await fits(page);
    await dialog.getByRole('button', { name: 'Replace api key' }).click();
    await expect(dialog).toBeHidden();
    await expectToast(page, 'API key replaced on pagerduty');

    await page.goto('/o/acme/connections/pagerduty');
    await page.getByRole('button', { name: 'Test' }).click();
    await expectToast(page, 'pagerduty passed its test');
    await expect(page.getByRole('region', { name: 'pagerduty failed its test' })).toHaveCount(0);
    await expect(page.getByText('a-fresh-pagerduty-key')).toHaveCount(0);
  });

  test('a member reads connections but changes nothing', async ({ page }) => {
    await page.request.post('/__mock/reset', { data: { role: 'member' } });
    await page.goto('/o/acme/connections');
    await expect(page.getByRole('heading', { level: 2, name: '8 connections' })).toBeVisible();
    await expect(page.getByRole('link', { name: /New connection/ })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Replace secret…' })).toHaveCount(0);
    // The audit log is for owners and admins.
    await expect(page.getByRole('region', { name: 'Recent activity' })).toHaveCount(0);
    await page.goto('/o/acme/connections/github');
    await expect(page.getByText('Owners and admins of Acme add, test and change connections.')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Test' })).toHaveCount(0);
    await expect(page.getByRole('button', { name: /Replace secret/ })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Save changes' })).toHaveCount(0);
    await fits(page);
  });
});

test.describe('New connection', () => {
  test('choose a kind, fill its fields, create, then test it on its page', async ({ page }) => {
    await page.goto('/o/acme/connections');
    await page.getByRole('link', { name: /New connection/ }).click();
    await expect(page).toHaveURL(/\/o\/acme\/connections\/new$/);
    await page.getByRole('searchbox', { name: 'Find a kind' }).fill('git');
    await page.getByRole('radio', { name: /^GitHub/ }).check();
    await expect(page).toHaveURL(/kind=github/);

    const form = page.getByRole('form', { name: 'GitHub details' });
    // github is taken: the name says so at once, and a free one is offered.
    await expect(form.getByLabel('Name')).toHaveValue('github-2');
    await form.getByLabel('Name').fill('github');
    await expect(form.getByText('Acme already has a connection named github.')).toBeVisible();
    await form.getByLabel('Name').fill('github-infra');
    const token = form.getByLabel('Token');
    await expect(token).toHaveAttribute('type', 'password');
    await token.fill(secretValue);
    await form.getByLabel('Description (optional)').fill('Pushes and pull requests on acme/infra');
    await expect(page.getByRole('region', { name: 'What a run gets' })).toContainText('GH_TOKEN');
    await fits(page);
    await form.getByRole('button', { name: 'Create connection' }).click();

    await expect(page).toHaveURL(/\/o\/acme\/connections\/github-infra$/);
    await expect(page.getByText('github-infra is created. Test it to check what you entered works.')).toBeVisible();
    // The callout is for the create: a reload or a shared link of the page does not say it again.
    await page.reload();
    await expect(page.getByText('github-infra is created.')).toHaveCount(0);
    await expect(page.getByText('Set', { exact: true })).toBeVisible();
    await expect(page.getByText(secretValue)).toHaveCount(0);
    await page.getByRole('button', { name: 'Test' }).click();
    await expectToast(page, 'github-infra passed its test');
    await fits(page);
  });

  test('a GitHub Enterprise host left on github.com’s API is refused, saying which field to set', async ({ page }) => {
    await page.goto('/o/acme/connections/new?kind=github');
    const form = page.getByRole('form', { name: 'GitHub details' });
    await form.getByLabel('Name').fill('github-ghe');
    await form.getByLabel('Token').fill(secretValue);
    await form.getByLabel('Host (optional)').fill('ghe.example.com');
    await form.getByRole('button', { name: 'Create connection' }).click();
    await expect(page.getByRole('alert')).toContainText(
      'test.url (from api_url) is on api.github.com, not on ghe.example.com or api.ghe.example.com',
    );
    await form.getByLabel('API URL (optional)').fill('https://ghe.example.com/api/v3');
    await form.getByRole('button', { name: 'Create connection' }).click();
    await expect(page).toHaveURL(/\/o\/acme\/connections\/github-ghe$/);
  });

  test('a required field is asked for before anything is sent', async ({ page }) => {
    await page.goto('/o/acme/connections/new?kind=jira');
    const form = page.getByRole('form', { name: 'Jira details' });
    await form.getByRole('button', { name: 'Create connection' }).click();
    await expect(form.getByText('Site is required.')).toBeVisible();
    await expect(form.getByText('API token is required.')).toBeVisible();
    await expect(page).toHaveURL(/\/connections\/new\?kind=jira$/);
  });

  test('without a master key goald seals nothing, and the page says what to set', async ({ page }) => {
    await page.request.post('/__mock/fail', {
      data: { path: '/connections', method: 'POST', status: 503, code: 'connections-unavailable' },
    });
    await page.goto('/o/acme/connections/new?kind=pagerduty');
    const form = page.getByRole('form', { name: 'PagerDuty details' });
    await form.getByLabel('API key').fill('a-key');
    await form.getByRole('button', { name: 'Create connection' }).click();
    await expect(form.getByRole('alert')).toContainText('goald has no master key');
    await expect(form.getByRole('alert')).toContainText('GOAL_MASTER_KEYS');
    await fits(page);
  });

  test('an older sign-in confirms it’s you, then creates it', async ({ page }) => {
    await page.request.post('/__mock/reset', { data: { auth: { mode: 'session', signed_in: 'stale' } } });
    await page.goto('/o/acme/connections/new?kind=pagerduty');
    const form = page.getByRole('form', { name: 'PagerDuty details' });
    await form.getByLabel('API key').fill('a-key');
    await form.getByRole('button', { name: 'Create connection' }).click();
    const reauth = page.getByRole('dialog', { name: 'Confirm it’s you' });
    await reauth.getByRole('button', { name: 'Email me a code' }).click();
    await reauth.getByLabel('Code').fill('000000');
    await expect(reauth).toBeHidden();
    await expect(page).toHaveURL(/\/o\/acme\/connections\/pagerduty-2$/);
  });

  test('define an organization kind and make a connection of it', async ({ page }) => {
    await page.goto('/o/acme/connections/new');
    await page.getByRole('button', { name: 'Define a kind…' }).click();
    const dialog = page.getByRole('dialog', { name: 'Define a kind' });
    await expect(dialog.getByLabel('Kind file (YAML)')).toHaveValue(/name: slack/);
    await fits(page);
    await dialog.getByRole('button', { name: 'Save kind' }).click();
    await expect(dialog).toBeHidden();
    await expect(page).toHaveURL(/kind=slack/);
    await expect(page.getByRole('radio', { name: /^Slack/ })).toBeChecked();
    await expect(page.getByText('12 platform kinds, 1 of Acme’s')).toBeVisible();
    await expect(page.getByRole('form', { name: 'Slack details' }).getByLabel('Bot token')).toBeVisible();
  });
});

test.describe('a connection’s page', () => {
  test('what a run gets, what uses it, its recent use; renaming and deleting wait while it is used', async ({
    page,
  }) => {
    await page.goto('/o/acme/connections/aws-prod');
    await expect(page).toHaveTitle('aws-prod · Connections · Acme · Goal');
    await expect(page.getByText('Agents get short-lived credentials per run.')).toBeVisible();
    await expect(page.getByRole('region', { name: 'What a run gets' })).toContainText('AWS_SESSION_TOKEN');
    await expect(page.getByRole('region', { name: 'Used by' })).toContainText('sre, its current version');
    await expect(page.getByRole('region', { name: 'Recent use' })).toContainText('Used by a run');
    await expect(page.getByLabel('Name')).toHaveAttribute('readonly', '');
    await expect(page.getByText('sre’s current version names it, so it can’t be renamed now.')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Delete…' })).toHaveAttribute('aria-disabled', 'true');
    await expect(page.getByText(/sre uses it, so Goal refuses until its definition no longer names it/)).toBeVisible();
    await fits(page);
  });

  test('change a field, then delete an unused connection by typing its name', async ({ page }) => {
    await page.goto('/o/acme/connections/gitlab-infra');
    await page.getByLabel('Description').fill('Infra repositories on our GitLab');
    await page.getByRole('button', { name: 'Save changes' }).click();
    await expectToast(page, 'Saved gitlab-infra');
    await expect(page.getByText('Infra repositories on our GitLab.')).toBeVisible();

    await page.getByRole('button', { name: 'Delete…' }).click();
    const dialog = page.getByRole('dialog', { name: 'Delete gitlab-infra?' });
    await fits(page);
    await dialog.getByLabel(/Type gitlab-infra to confirm/).fill('gitlab-infra');
    await dialog.getByRole('button', { name: 'Delete connection' }).click();
    await expect(page).toHaveURL(/\/o\/acme\/connections$/);
    await expect(page.getByRole('heading', { level: 2, name: '7 connections' })).toBeVisible();
  });

  test('replacing a secret keeps the field edits not saved yet', async ({ page }) => {
    await page.goto('/o/acme/connections/pagerduty');
    await page.getByLabel('Description').fill('Paging for the on-call');
    await page.getByRole('button', { name: /^Replace secret…/ }).click();
    const dialog = page.getByRole('dialog', { name: 'Replace api key of pagerduty' });
    await dialog.getByLabel('API key').fill('a-fresh-pagerduty-key');
    await dialog.getByRole('button', { name: 'Replace api key' }).click();
    await expectToast(page, 'API key replaced on pagerduty');
    await expect(page.getByText('Set', { exact: true })).toBeVisible();
    await expect(page.getByLabel('Description')).toHaveValue('Paging for the on-call');
    await page.getByRole('button', { name: 'Save changes' }).click();
    await expectToast(page, 'Saved pagerduty');
    await expect(page.getByText('Paging for the on-call.')).toBeVisible();
  });

  test('a rename opens the new name with what a run gets, and reads the old name no more', async ({ page }) => {
    await page.goto('/o/acme/connections/gitlab-infra');
    const gets = page.getByRole('region', { name: 'What a run gets' });
    await expect(gets.locator('code').first()).toBeVisible();
    const env = (await gets.locator('code').allTextContents()).filter(Boolean);
    expect(env.length).toBeGreaterThan(0);
    const oldName: string[] = [];
    page.on('request', (r) => {
      if (r.method() === 'GET' && new URL(r.url()).pathname.endsWith('/connections/gitlab-infra'))
        oldName.push(r.url());
    });
    await page.getByLabel('Name').fill('gitlab-ops');
    await page.getByRole('button', { name: 'Save changes' }).click();
    await expectToast(page, 'Saved gitlab-ops');
    await expect(page).toHaveURL(/\/o\/acme\/connections\/gitlab-ops$/);
    await expect(gets.locator('code')).toHaveText(env);
    await page.waitForLoadState('networkidle');
    expect(oldName).toEqual([]);
  });

  test('an unknown connection is not found', async ({ page }) => {
    await page.goto('/o/acme/connections/nothing-here');
    await expect(page.getByRole('heading', { name: 'No such connection' })).toBeVisible();
  });
});

test.describe('the builder', () => {
  const basics = '/o/acme/f/goal/definition/basics';
  const connections = '/o/acme/f/goal/definition/connections';
  const status = (page: Page) => page.getByRole('status').filter({ hasText: /Based on|runs now/ });

  test('Basics: the runtime’s model connection, size, tools and repository connection', async ({ page }) => {
    await page.goto(basics);
    await expect(page.getByRole('heading', { level: 3, name: 'Runtime' })).toBeVisible();
    const model = page.getByRole('combobox', { name: 'Model connection' });
    await expect(model).toHaveValue('claude');
    await expect(page.getByRole('combobox', { name: 'Repository connection' })).toHaveValue('github');
    await expect(page.getByRole('radio', { name: 'Large' })).toBeChecked();

    await page.getByRole('radio', { name: 'Medium' }).check();
    const tool = page.getByRole('textbox', { name: 'Add a tool' });
    await tool.fill('rm -rf /');
    await tool.press('Enter');
    await expect(page.getByText('A mise tool spec, such as go, go@1.26.10 or npm:@acme/cli@2.')).toBeVisible();
    await tool.fill('task@3.46.1');
    await tool.press('Enter');
    await expect(page.getByRole('button', { name: 'Remove tool task@3.46.1' })).toBeVisible();
    await expect(status(page)).toContainText('Autosaved');
    await fits(page);

    await page.reload();
    await expect(page.getByRole('radio', { name: 'Medium' })).toBeChecked();
    await expect(page.getByRole('button', { name: 'Remove tool task@3.46.1' })).toBeVisible();
  });

  test('Connections: tick an organization connection and give it to an agent', async ({ page }) => {
    await page.goto(connections);
    await expect(page).toHaveTitle('Connections · Definition · goal · Acme · Goal');
    await expect(page.getByRole('heading', { level: 2, name: 'Connections and knowledge' })).toBeVisible();
    await expect(page.getByRole('checkbox', { name: /^github/ })).toBeChecked();
    await expect(page.getByRole('checkbox', { name: /^playwright/ })).toBeChecked();
    await expect(page.getByRole('checkbox', { name: 'reviewer gets github' })).toBeChecked();
    const runsGet = page.getByRole('region', { name: 'What runs get' });
    await expect(runsGet).toContainText('Model access, from Basics');
    await expect(
      page.getByRole('region', { name: 'Knowledge goal may search' }).getByRole('checkbox', { name: /^Handbook/ }),
    ).not.toBeChecked();
    await fits(page);

    await page.getByRole('checkbox', { name: /^datadog/ }).check();
    await page.getByRole('checkbox', { name: 'backend-engineer gets datadog' }).check();
    await expect(runsGet).toContainText('DD_API_KEY');
    await expect(status(page)).toContainText('Autosaved');
    await page.reload();
    await expect(page.getByRole('checkbox', { name: 'backend-engineer gets datadog' })).toBeChecked();

    // Unticking takes it from every agent too.
    await page.getByRole('checkbox', { name: /^datadog/ }).uncheck();
    await expect(page.getByRole('checkbox', { name: 'backend-engineer gets datadog' })).toHaveCount(0);
  });

  test('Connections: while the organization’s connections load it says so, never that it has none', async ({
    page,
    request,
  }) => {
    await request.post('/__mock/fail', { data: { path: '/connections', method: 'GET', delay_ms: 2500, times: 0 } });
    await page.goto(connections);
    const uses = page.getByRole('region', { name: 'Connections goal uses' });
    await expect(uses.getByText('Loading Acme’s connections…')).toBeVisible();
    await expect(page.getByText(/has no connections yet|not one of Acme’s connections/)).toHaveCount(0);
    await expect(
      page.getByRole('region', { name: 'Per agent' }).getByText('Loading Acme’s connections…'),
    ).toBeVisible();
    await expect(uses.getByRole('checkbox', { name: /^github/ })).toBeChecked({ timeout: 10_000 });
  });

  test('Connections: two connections setting one variable for an agent, and the fix that keeps one', async ({
    page,
  }) => {
    const made = await page.request.post('/v1/orgs/acme/connections', {
      data: { name: 'github-docs', kind: 'github', secrets: { token: 'a-token' } },
    });
    expect(made.status()).toBe(201);
    await page.goto(connections);
    await page.getByRole('checkbox', { name: /^github-docs/ }).check();
    await page.getByRole('checkbox', { name: 'reviewer gets github-docs' }).check();
    const problem = page
      .getByText(/agent reviewer: github and github-docs both set GH_HOST, GH_TOKEN/)
      .filter({ visible: true });
    await expect(problem).toBeVisible();
    await fits(page);
    await page.getByRole('button', { name: 'Keep github' }).filter({ visible: true }).click();
    await expect(problem).toHaveCount(0);
    await expect(page.getByRole('checkbox', { name: 'reviewer gets github-docs' })).not.toBeChecked();
  });

  test('the wizard’s Connections step is the same section', async ({ page }) => {
    test.skip(isPhone(page), 'one width is enough for the shared section');
    await page.goto('/o/acme/f/sre/definition/connections');
    await expect(page.getByRole('heading', { level: 3, name: 'Connections sre uses' })).toBeVisible();
    await expect(page.getByRole('checkbox', { name: /^aws-prod/ })).toBeChecked();
    await expect(page.getByRole('checkbox', { name: 'investigator gets pagerduty' })).toBeChecked();
  });
});
