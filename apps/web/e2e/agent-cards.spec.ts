// D12: the definition's cards on the Agent page (Agent and Mobile-Agent boards), read from the
// version that runs. sre's investigator has three skills and legacy tool rules; goal's reviewer
// has neither and gets the worker's read-only tools.
import { expect, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll, isPhone } from './support';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

test('skills, tool policy and Edit in Definition come from the version that runs', async ({ page }) => {
  await page.goto('/o/acme/f/sre/agents/investigator');
  await expect(page.getByRole('heading', { level: 1, name: 'investigator' })).toBeVisible();
  // The role from the definition leads the sentence beside the sidebar; phones keep it short.
  const role = page.getByText('Gathers evidence from AWS and the repository and proposes a diagnosis.');
  if (isPhone(page)) await expect(role).toBeHidden();
  else await expect(role).toBeVisible();

  const skills = page.getByRole('region', { name: 'Skills' });
  await expect(skills).toContainText('Copied into this agent’s container only');
  const rows = skills.getByRole('listitem');
  await expect(rows).toHaveCount(3);
  await expect(rows.nth(0)).toContainText('aws-investigation');
  await expect(rows.nth(0)).toContainText('Read-only playbook for ECS, RDS, ALB, Lambda');
  await expect(rows.nth(0)).toContainText('Source: local');
  await expect(rows.nth(1)).toContainText('Severity and sizing rules');
  await expect(rows.nth(2)).toContainText('Used when the item becomes a postmortem');
  await expect(rows.nth(2)).toContainText('Source: skills.sh');

  const tools = page.getByRole('region', { name: 'Tool policy' });
  const allowed = tools.getByRole('list', { name: 'Allowed' }).getByRole('listitem');
  await expect(allowed).toHaveText(['Read', 'Grep', 'Glob', 'Bash', 'WebFetch']);
  const denied = tools.getByRole('list', { name: 'Denied before the call runs' }).getByRole('listitem');
  await expect(denied).toHaveText([
    'Bash(aws * delete*)',
    'Bash(aws * put*)',
    'Bash(terraform apply*)',
    'Bash(kubectl delete*)',
  ]);
  // E's line about blocked calls sits under the policy, once.
  await expect(tools.getByText('Blocked this week: 2 calls, both aws ssm put-parameter.')).toBeVisible();
  await expect(page.getByText(/^Blocked this week/)).toHaveCount(1);

  await expect(page.getByRole('link', { name: 'Edit in Definition' })).toHaveAttribute(
    'href',
    '/o/acme/f/sre/definition/agents?agent=investigator',
  );
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('“Add a skill from skills.sh” opens Definition › Agents with the picker focused (flow 8)', async ({ page }) => {
  await page.goto('/o/acme/f/sre/agents/investigator');
  await page.getByRole('region', { name: 'Skills' }).getByRole('link', { name: 'Add a skill from skills.sh' }).click();
  await expect(page).toHaveURL(/\/o\/acme\/f\/sre\/definition\/agents\?agent=investigator&add=skill$/);
  await expect(page.getByRole('heading', { level: 3, name: 'investigator' })).toBeVisible();
  const picker = page.getByRole('combobox', { name: 'Add a skill from the catalog' });
  await expect(picker).toBeFocused();
  await expect(page.getByRole('listbox', { name: 'Skills' })).toBeVisible();
  await expectAccessible(page);
});

test('cards that cannot read the running version say so, and Try again reads it', async ({ page, request }) => {
  await request.post('/__mock/fail', {
    data: { path: '/versions/current/model', method: 'GET', status: 404, code: 'version-not-found', times: 0 },
  });
  await page.goto('/o/acme/f/sre/agents/investigator');
  // The statistics never wait for the definition.
  await expect(page.getByRole('heading', { level: 1, name: 'investigator' })).toBeVisible();
  const skills = page.getByRole('region', { name: 'Skills' });
  await expect(skills).toContainText('The version that runs can’t be read right now.');
  await expect(page.getByRole('region', { name: 'Tool policy' })).toContainText('can’t be read right now');
  await expectAccessible(page);

  await request.delete('/__mock/fail');
  await skills.getByRole('button', { name: 'Try again' }).click();
  await expect(skills.getByRole('listitem')).toHaveCount(3);
  await expect(page.getByRole('list', { name: 'Allowed' })).toBeVisible();
});

test('an agent without settings reads; a draft that changes it says so and links to Review', async ({ page }) => {
  await page.goto('/o/acme/f/goal/agents/reviewer');
  const skills = page.getByRole('region', { name: 'Skills' });
  await expect(skills.getByText('No skills. The agent works from its prompt alone.')).toBeVisible();
  const tools = page.getByRole('region', { name: 'Tool policy' });
  await expect(tools.getByRole('list', { name: 'Allowed' }).getByRole('listitem')).toHaveText(['Read', 'Grep', 'Glob']);
  await expect(tools.getByRole('list', { name: 'Denied before the call runs' })).toContainText('Bash');
  // goal's open draft leaves the reviewer alone.
  await expect(page.getByText('The draft changes this agent.', { exact: false })).toBeHidden();

  // Give it a skill in the draft: the page still shows what runs, and points at Review.
  await page.getByRole('link', { name: 'Edit in Definition' }).click();
  await expect(page).toHaveURL(/\/definition\/agents\?agent=reviewer$/);
  await page.getByRole('combobox', { name: 'Add a skill from the catalog' }).click();
  await page.getByRole('option', { name: 'acme-conventions' }).click();
  await expect(page.getByRole('button', { name: 'Remove acme-conventions' })).toBeVisible();
  await expect(page.getByRole('status').filter({ hasText: /Based on|runs now/ })).toContainText('Autosaved');

  await page.getByRole('link', { name: 'Open agent page' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'reviewer' })).toBeVisible();
  await expect(page.getByText('The draft changes this agent.', { exact: false })).toBeVisible();
  await expect(skills.getByText('No skills. The agent works from its prompt alone.')).toBeVisible();
  await expectAccessible(page);
  await page.getByRole('link', { name: 'Review the draft' }).click();
  await expect(page).toHaveURL(/\/o\/acme\/f\/goal\/definition\/review$/);
});
