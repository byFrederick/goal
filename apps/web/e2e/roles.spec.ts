// Role-aware screens (spec 3 §10.2, PR 3-14; canvas Board-Viewer, Define-View, Define-Basics,
// New-Basics): what each role sees offered, from goald's `can`, at every width with axe. The mock
// plays the caller in a role (POST /__mock/reset {role}, mock/roles.ts) and refuses what the role
// lacks with 403 forbidden, as goald does, so each test also checks the server refuses anyway.
import { expect, type Page, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll, isPhone, orgNav } from './support';

type Role = 'owner' | 'admin' | 'member' | 'editor' | 'approver' | 'viewer';

const as = (role: Role) =>
  test.beforeEach(async ({ request }) => {
    await request.post('/__mock/reset', { data: { role } });
  });

/** The andon cord on the board, as the current width shows it. */
const andon = (page: Page) =>
  page.getByRole('button', { name: /^(Pull the andon cord|Clear the andon)$/ }).filter({ visible: true });

const newFactory = (page: Page) => orgNav(page).filter({ visible: true }).getByRole('link', { name: 'New factory' });

test.describe('a viewer', () => {
  as('viewer');

  test('sees the board without moving cards, submitting work or pulling the cord', async ({ page }) => {
    await page.goto('/o/acme/f/sre/board');
    await expect(page.getByRole('heading', { level: 1, name: 'SRE board' })).toBeVisible();
    await expect(page.getByText('You’re a viewer of sre')).toBeVisible();

    await expect(andon(page)).toHaveAttribute('aria-disabled', 'true');
    await expect(andon(page)).toHaveAccessibleDescription('Approvers and editors can pull the cord');
    // aria-disabled keeps it focusable: Playwright waits for it to be enabled unless forced.
    await andon(page).click({ force: true });
    await expect(page.getByRole('dialog')).toHaveCount(0);

    await expect(page.getByRole('button', { name: /^Move / })).toHaveCount(0);
    await expect(page.getByRole('main').getByRole('button', { name: /New item/ })).toHaveCount(0);
    await expect(
      page.getByText(
        'You can see sre’s board. Approvers and editors of sre move cards and decide items; agents pull work, nobody assigns it.',
      ),
    ).toBeVisible();
    await expect(page.getByText('Waiting on an approver').filter({ visible: true }).first()).toBeVisible();
    await expect(newFactory(page)).toHaveCount(0);
    await expectAccessible(page);
    await expectNoHorizontalScroll(page);
  });

  test('sees decisions on an item disabled with who decides, and goald refuses them anyway', async ({
    page,
    request,
  }) => {
    await page.goto('/o/acme/f/goal/items/12');
    await expect(page.getByRole('heading', { level: 1, name: 'goald: serve the embedded web UI' })).toBeVisible();
    const approve = page.getByRole('button', { name: /^Approve/ }).filter({ visible: true });
    await expect(approve).toHaveAttribute('aria-disabled', 'true');
    await expect(approve).toHaveAccessibleDescription('Approvers of goal decide here.');
    await approve.click({ force: true });
    await expect(page.getByText('Waiting on an approver').first()).toBeVisible();
    await expect(page.getByRole('button', { name: 'Pause' })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Move' })).toHaveCount(0);
    await expect(page.getByRole('textbox', { name: /^Note to/ })).toHaveCount(0);
    await expect(page.getByText('Approvers and editors of goal send notes to its agents.').first()).toBeAttached();
    await expectAccessible(page);
    await expectNoHorizontalScroll(page);

    const refused = await request.post('/v1/orgs/acme/items/goal%2312/approve', { data: {} });
    expect(refused.status()).toBe(403);
    expect(await refused.json()).toMatchObject({ code: 'forbidden' });
  });

  test('reads the Definition view only: the running version, no edits, who approves', async ({ page, request }) => {
    await page.goto('/o/acme/f/goal/definition/basics');
    await expect(page.getByText('View only. Editors of goal change its definition.')).toBeVisible();
    await expect(page.getByText('v8, running')).toBeVisible();
    await expect(page.getByRole('link', { name: 'Review changes' })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Discard draft', exact: true })).toHaveCount(0);
    await expect(page.getByRole('navigation', { name: 'Definition sections' }).getByRole('link')).toHaveCount(4);

    const name = page.getByRole('textbox', { name: 'Name' });
    await expect(name).toHaveValue('goal');
    await expect(name).toHaveAttribute('readonly', '');
    await expect(page.getByRole('button', { name: /^Remove work type/ })).toHaveCount(0);
    await expect(page.getByRole('heading', { name: 'Who approves' })).toBeVisible();
    await expect(
      page.getByText(
        'Owners and admins of Acme, and this factory’s approvers and editors: F. Cruz, M. Okafor, J. Lindqvist.',
      ),
    ).toBeVisible();
    // Only whoever manages people changes it.
    await expect(page.getByRole('link', { name: 'Change in Members' })).toHaveCount(0);
    await expectAccessible(page);
    await expectNoHorizontalScroll(page);

    await page.goto('/o/acme/f/goal/definition/line');
    await expect(page.getByRole('heading', { level: 2, name: 'Line', exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Add station' })).toHaveCount(0);
    await expect(page.getByRole('button', { name: /^More for / })).toHaveCount(0);
    await expect(page.getByRole('button', { name: /^Move .* (up|down)$/ })).toHaveCount(0);
    await expect(page.getByText('Approvers of goal').first()).toBeVisible();
    await expect(page.getByRole('textbox', { name: 'Name of station 2' })).toHaveAttribute('readonly', '');
    await expectAccessible(page);

    await page.goto('/o/acme/f/goal/definition/review');
    await expect(page.getByText('Editors of goal review and publish its definition.')).toBeVisible();
    await expect(page.getByRole('button', { name: /Publish/ })).toHaveCount(0);

    const refused = await request.put('/v1/orgs/acme/factories/goal/draft/files/factory.yaml', { data: 'x' });
    expect(refused.status()).toBe(403);
  });
});

test.describe('an approver', () => {
  as('approver');

  test('moves and decides, pulls the cord, and reads the Definition view only', async ({ page }) => {
    await page.goto('/o/acme/f/sre/board');
    await expect(page.getByText('You’re an approver of sre')).toBeVisible();
    await expect(andon(page)).not.toHaveAttribute('aria-disabled', 'true');
    await expect(
      page
        .getByRole('button', { name: /^Move / })
        .filter({ visible: true })
        .first(),
    ).toBeVisible();
    await expect(page.getByRole('main').getByRole('button', { name: /New item/ })).toBeVisible();

    await page.goto('/o/acme/f/goal/items/12');
    const approve = page.getByRole('button', { name: /^Approve/ }).filter({ visible: true });
    await expect(approve).toBeEnabled();
    await expect(approve).not.toHaveAttribute('aria-disabled', 'true');

    await page.goto('/o/acme/f/goal/definition/basics');
    await expect(page.getByText('View only. Editors of goal change its definition.')).toBeVisible();
    await expectAccessible(page);
  });
});

test.describe('an editor', () => {
  as('editor');

  test('edits the Definition but starts no factory', async ({ page }) => {
    await page.goto('/o/acme/f/goal/definition/basics');
    await expect(page.getByRole('link', { name: 'Review changes', exact: true }).first()).toBeVisible();
    await expect(page.getByText(/^View only/)).toHaveCount(0);
    await expect(page.getByRole('textbox', { name: 'Name' })).not.toHaveAttribute('readonly', '');
    await expect(page.getByText('You’re a viewer of goal')).toHaveCount(0);
    await expect(newFactory(page)).toHaveCount(0);

    await page.goto('/o/acme/new/start');
    await expect(page.getByText('Only owners and admins start factories')).toBeVisible();
    await expectAccessible(page);
    await expectNoHorizontalScroll(page);
  });
});

test.describe('a member without factory roles', () => {
  as('member');

  test('sees no factory and the organization settings view only', async ({ page }) => {
    await page.goto('/o/acme');
    await expect(page.getByText('No factories to show yet')).toBeVisible();
    await expect(page.getByRole('link', { name: 'New factory' })).toHaveCount(0);
    await expectAccessible(page);

    await page.goto('/o/acme/settings');
    await expect(page.getByText('View only. Owners and admins of Acme change its settings.')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Save changes' })).toHaveCount(0);
    await expect(page.getByLabel('Name')).toHaveAttribute('readonly', '');
    await expect(page.getByRole('heading', { name: 'Danger zone' })).toHaveCount(0);
    await expectAccessible(page);
    await expectNoHorizontalScroll(page);
  });
});

test.describe('an admin', () => {
  as('admin');

  test('changes settings and starts factories, but has no Danger zone', async ({ page }) => {
    await page.goto('/o/acme/settings');
    await expect(page.getByRole('button', { name: 'Save changes' })).toBeVisible();
    await expect(page.getByText(/^View only/)).toHaveCount(0);
    await expect(page.getByRole('heading', { name: 'Danger zone' })).toHaveCount(0);
    await expect(newFactory(page)).toBeVisible();
  });
});

test.describe('an owner', () => {
  as('owner');

  test('has every control, the Danger zone included', async ({ page }) => {
    await page.goto('/o/acme/settings');
    await expect(page.getByRole('heading', { name: 'Danger zone' })).toBeVisible();
    await page.goto('/o/acme/f/sre/board');
    await expect(andon(page)).not.toHaveAttribute('aria-disabled', 'true');
    await expect(page.getByText(/^You’re an? /)).toHaveCount(0);
  });
});

test('the local user of GOAL_AUTH=none is offered everything, with no role named', async ({ page, request }) => {
  await request.post('/__mock/reset');
  await page.goto('/o/acme/f/sre/board');
  await expect(andon(page)).not.toHaveAttribute('aria-disabled', 'true');
  await expect(page.getByText(/^You’re an? /)).toHaveCount(0);
  if (!isPhone(page)) await expect(page.getByRole('button', { name: /^Move / }).first()).toBeVisible();
  await expect(newFactory(page)).toBeVisible();
});

test.describe('factory roles on the settings page', () => {
  as('owner');

  test('an owner adds a person to a factory role, changes it and removes it', async ({ page }) => {
    await page.goto('/o/acme/f/goal/settings');
    const people = page.getByRole('region', { name: 'People with access' });
    const row = people
      .locator('div')
      .filter({ has: page.getByText('A. Ortiz', { exact: true }) })
      .last();

    await people.getByRole('button', { name: 'Add people' }).click();
    const add = page.getByRole('dialog', { name: 'Add people' });
    await add.getByLabel('Person').selectOption({ label: 'A. Ortiz' });
    await add.getByLabel('Role').selectOption('approver');
    await add.getByRole('button', { name: 'Add', exact: true }).click();
    await expect(people.getByText('Approves and decides')).toBeVisible();

    await row.getByRole('button', { name: 'Change role' }).click();
    const change = page.getByRole('dialog', { name: /^A\. Ortiz on / });
    await change.getByLabel('Role').selectOption('editor');
    await change.getByRole('button', { name: 'Save' }).click();
    await expect(people.getByText('Edits the definition and creates items')).toBeVisible();

    await row.getByRole('button', { name: 'Change role' }).click();
    await page
      .getByRole('dialog', { name: /^A\. Ortiz on / })
      .getByRole('button', { name: 'Remove from factory' })
      .click();
    await expect(people.getByText('A. Ortiz', { exact: true })).toHaveCount(0);
    await expectAccessible(page);
  });
});
