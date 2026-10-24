// Definition › Connections before phase 4 (D9) against the mock: goal's draft has one legacy
// connection, github (a remote MCP server signing in with env:GOAL_GITHUB_TOKEN), used by reviewer.
import { expect, type Page, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll } from './support';

const connections = '/o/acme/f/goal/definition/connections';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

const status = (page: Page) => page.getByRole('status').filter({ hasText: /Based on|runs now/ });
const tile = (page: Page, name: RegExp) =>
  page.getByRole('navigation', { name: 'Definition sections' }).getByRole('link', { name });
const access = (page: Page) => page.getByRole('group', { name: 'Agents allowed to use github' });

/** The list, then github's detail (on a phone the list comes first). */
async function openGithub(page: Page) {
  await page.goto(connections);
  await page
    .getByRole('navigation', { name: 'Connections' })
    .getByRole('link', { name: /github/ })
    .click();
  await expect(page).toHaveURL(/\?connection=github$/);
  await expect(page.getByRole('heading', { level: 3, name: 'github' })).toBeVisible();
}

test('the connections list, how each signs in, and the catalog placeholder', async ({ page }) => {
  await page.goto(connections);
  await expect(page).toHaveTitle('Connections · Definition · goal · Acme · Goal');
  const list = page.getByRole('navigation', { name: 'Connections' });
  await expect(list.getByRole('heading', { name: 'Added to goal' })).toBeVisible();
  const github = list.getByRole('link', { name: /github/ });
  await expect(github).toContainText('Remote MCP server, env:GOAL_GITHUB_TOKEN');
  await expect(github).toContainText('No problems');

  const placeholder = page.getByRole('region', { name: 'Add from the catalog' });
  await expect(placeholder).toContainText(
    'Connectors arrive with the connector catalog. Until then, add a connection in YAML.',
  );
  await expect(placeholder.getByRole('link', { name: 'Open YAML at connections' })).toHaveAttribute(
    'href',
    /mode=yaml&line=\d+/,
  );
  // No tools table, health or test call before phase 4.
  await expect(page.getByRole('table')).toHaveCount(0);
  await expect(page.getByText('Connection health')).toHaveCount(0);
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);

  await openGithub(page);
  const detail = page.getByRole('region', { name: 'github', exact: true });
  await expect(detail).toContainText('env:GOAL_GITHUB_TOKEN, a platform secret of this install.');
  await expect(detail).toContainText('No problems');
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('agent access is saved in each agent’s connections', async ({ page }) => {
  await openGithub(page);
  await expect(access(page).getByRole('checkbox', { name: 'reviewer' })).toBeChecked();
  await access(page).getByRole('checkbox', { name: 'backend-engineer' }).check();
  await access(page).getByRole('checkbox', { name: 'reviewer' }).uncheck();

  await expect(status(page)).toContainText('Autosaved');
  await expect(tile(page, /^Agents, \d+ changes?, 1 problem$/)).toBeAttached();

  await page.reload();
  await expect(access(page).getByRole('checkbox', { name: 'backend-engineer' })).toBeChecked();
  await expect(access(page).getByRole('checkbox', { name: 'reviewer' })).not.toBeChecked();
});

test('Remove names the agents that lose the connection and removes every reference', async ({ page }) => {
  await openGithub(page);
  await page.getByRole('button', { name: 'Remove…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Remove github?' });
  await expect(dialog).toContainText('reviewer loses it.');
  await expectAccessible(page);
  await dialog.getByRole('button', { name: 'Remove connection' }).click();

  await expect(page).toHaveURL(/\/definition\/connections$/);
  await expect(page.getByText('No connections. Agents can still read and edit the workspace.')).toBeVisible();
  await expect(status(page)).toContainText('Autosaved');
  await expect(tile(page, /^Connections, 1 change$/)).toBeAttached();

  // reviewer's reference went in the same edit: the agents tile reports no unknown connection.
  await page.reload();
  await expect(page.getByText('No connections. Agents can still read and edit the workspace.')).toBeVisible();
  await expect(tile(page, /^Agents, \d+ changes?, 1 problem$/)).toBeAttached();
});
