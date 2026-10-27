// The New factory wizard (D13) against the mock: Start (blank, a template, a clone, Import YAML),
// the steps over the new factory's draft, Save and exit, resuming from the Factories home and
// deleting a draft factory. The mock's catalog has five templates and no blank one, so a blank
// start is goald's skeleton.
import { expect, type Page, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll, isPhone } from './support';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

const saved = (page: Page) => page.getByRole('status').filter({ hasText: /^Draft saved/ });
const steps = (page: Page) => page.getByRole('navigation', { name: 'New factory steps' });
const continueTo = (page: Page, step: string) => page.getByRole('button', { name: `Continue to ${step}` });
const factoryId = (page: Page) => new URL(page.url()).searchParams.get('factory') ?? '';

async function checkPage(page: Page) {
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
}

/** Replaces the text of a code editor: Monaco on a desktop, the textarea on a phone. */
async function typeInto(page: Page, name: string, text: string) {
  await page.getByRole('textbox', { name }).focus();
  await page.keyboard.press('ControlOrMeta+A');
  await page.keyboard.press('Delete');
  await page.keyboard.type(text);
}

/** An agent's detail: on a phone the list comes first. */
async function openAgent(page: Page, agent: string) {
  if (isPhone(page)) {
    await page
      .getByRole('navigation', { name: 'Agents' })
      .getByRole('link', { name: new RegExp(agent) })
      .click();
  }
  await expect(page.getByRole('heading', { level: 3, name: agent })).toBeVisible();
}

test('a factory from a template, step by step (flow 1)', async ({ page }) => {
  await page.goto('/o/acme');
  await page.getByRole('main').getByRole('link', { name: 'New factory' }).first().click();
  await expect(page).toHaveURL(/\/o\/acme\/new\/start$/);
  await expect(page).toHaveTitle('Start · New factory · Acme · Goal');
  await expect(page.getByRole('heading', { level: 1, name: 'New factory' })).toBeVisible();
  if (isPhone(page)) await expect(steps(page)).toContainText('Step 1 of 6 · Start');
  else await expect(steps(page).locator('[aria-current="step"]')).toHaveText('1Start');

  // From a template is chosen; Cost review's detail shows its line, agents and connectors.
  await expect(page.getByRole('radio', { name: /^From a template/ })).toBeChecked();
  await expect(page.getByText('5 in the bucket')).toBeVisible();
  await page.getByRole('radio', { name: /^Cost review/ }).check();
  const detail = page.getByRole('region', { name: 'Cost review' });
  await expect(detail).toContainText('Template v2, last changed Oct 9');
  await expect(detail.getByRole('list', { name: 'The line of Cost review' }).getByRole('listitem')).toHaveCount(6);
  await expect(detail.getByText('Approve, a person decides')).toBeVisible();
  await expect(detail).toContainText('cost-analyst reads the cost report, proposer writes the change and reviewer');
  await expect(page.getByRole('textbox', { name: 'Name' })).toHaveValue('cost-review');
  await checkPage(page);

  // Continue creates the factory and opens Basics on its draft.
  await continueTo(page, 'Basics').click();
  await expect(page).toHaveURL(/\/o\/acme\/new\/basics\?factory=[0-9a-f-]+$/);
  await expect(page).toHaveTitle('Basics · New factory · Acme · Goal');
  await expect(page.getByRole('heading', { level: 1, name: 'cost-review' })).toBeVisible();
  await expect(page.getByText('From the Cost review template.', { exact: false })).toBeVisible();
  if (!isPhone(page)) {
    await expect(steps(page).getByRole('link', { name: 'Line' })).toBeVisible();
    await expect(steps(page).getByText('Start, done')).toBeAttached();
  }

  await page.getByRole('textbox', { name: 'Name', exact: true }).fill('cloud-costs');
  const addType = page.getByRole('textbox', { name: 'Add a work type' });
  await addType.fill('finding');
  await addType.press('Enter');
  await page.getByRole('textbox', { name: 'Repository URL' }).fill('https://github.com/acme/infra');
  await page.getByRole('textbox', { name: 'Repository URL' }).blur();
  // The template names no model connection, which an agent's runs need: publish waits for one.
  await page.getByRole('combobox', { name: 'Model connection' }).selectOption('claude');
  await expect(saved(page)).toBeVisible();
  await expect(page.getByRole('heading', { level: 1, name: 'cloud-costs' })).toBeVisible();
  await checkPage(page);

  // Line: add a station, move it by keys and by button, raise a WIP, turn on the merge step.
  await continueTo(page, 'Line').click();
  await expect(page.getByRole('heading', { level: 2, name: 'Line', exact: true })).toBeFocused();
  const stations = page.getByRole('list', { name: 'Stations, in order' });
  const row = (name: string) => stations.getByRole('listitem', { name: new RegExp(`^\\d+\\. ${name},`) });
  await row('Approve')
    .getByRole('textbox', { name: /^Name of station/ })
    .focus();
  await page.getByRole('button', { name: 'Add station' }).click();
  await page.getByRole('menuitem', { name: 'Agent station' }).click();
  await page.keyboard.type('Security review');
  await expect(row('Security review')).toHaveAccessibleName('6. Security review, agent station');
  await page.keyboard.press('Alt+ArrowUp');
  await expect(row('Security review')).toHaveAccessibleName('5. Security review, agent station');
  await row('Security review').getByRole('button', { name: 'Move Security review up' }).click();
  await expect(row('Security review')).toHaveAccessibleName('4. Security review, agent station');
  await row('Propose').getByRole('button', { name: 'Raise the WIP limit of Propose' }).click();
  await expect(row('Propose').getByRole('textbox', { name: 'WIP limit of Propose' })).toHaveValue('2');
  const merge = row('Approve').getByRole('switch', { name: 'Merge step at Approve' });
  await merge.click();
  await expect(merge).toHaveAttribute('aria-checked', 'true');
  await expect(saved(page)).toBeVisible();
  await checkPage(page);

  // Agents: a model, the prompt, a skill from skills.sh.
  await continueTo(page, 'Agents').click();
  await openAgent(page, 'cost-analyst');
  const agent = page.getByRole('region', { name: 'cost-analyst', exact: true });
  await agent.getByRole('combobox', { name: 'Model' }).selectOption('claude-opus-5-5');
  await typeInto(page, 'Prompt of cost-analyst', '# cost-analyst\n\nFind the three largest avoidable costs.');
  await agent.getByRole('combobox', { name: 'Add a skill from the catalog' }).fill('aws');
  await page.getByRole('option', { name: /aws-cost-explorer/ }).click();
  const dialog = page.getByRole('dialog', { name: 'Add aws-cost-explorer' });
  await dialog.getByRole('button', { name: 'Add skill' }).click();
  await expect(agent.getByRole('button', { name: 'Remove aws-cost-explorer' })).toBeVisible();
  await expect(saved(page)).toBeVisible();
  await checkPage(page);

  // Connections: the template's github connection, given to the proposer.
  await continueTo(page, 'Connections').click();
  await expect(page.getByRole('heading', { level: 2, name: 'Connections and knowledge' })).toBeVisible();
  await expect(page.getByRole('checkbox', { name: /^github/ })).toBeChecked();
  await page.getByRole('checkbox', { name: 'proposer gets github' }).check();
  await expect(page.getByRole('checkbox', { name: 'proposer gets github' })).toBeChecked();
  await expect(saved(page)).toBeVisible();
  await checkPage(page);

  // Review: the checks, the changes from the template, then Publish v1; the Floor opens.
  await continueTo(page, 'Review and publish').click();
  await expect(page).toHaveURL(/\/o\/acme\/new\/review\?factory=/);
  await expect(page.getByRole('region', { name: 'Checks' })).toBeVisible();
  const changes = page.getByRole('region', { name: 'Changes from the Cost review template' });
  await expect(changes).toContainText(/\d+ changes in \d+ sections/);
  await expect(changes).toContainText('factory.yaml');
  await expect(page.getByRole('button', { name: 'Back' })).toHaveCount(0);
  await expect(page.getByRole('link', { name: 'Back' })).toBeVisible();
  await checkPage(page);
  await page.getByRole('checkbox', { name: /^Start the line right away/ }).check();
  await page.getByRole('button', { name: 'Publish v1' }).click();
  await expect(page).toHaveURL(/\/o\/acme\/f\/[^/]+$/);
  await expect(page.getByRole('heading', { level: 1 })).toContainText(/Cost review|cloud-costs/);
  // The Floor has the published line at once, not the factory as it was before its first version
  // (New item is in the Floor's header from 768 px).
  if (isPhone(page)) return;
  await page.getByRole('main').getByRole('button', { name: 'New item' }).click();
  const newItem = page.getByRole('dialog', { name: 'New item' });
  await expect(newItem).toContainText(/It enters \w+ and waits to be pulled\./);
  await expect(newItem).not.toContainText('It enters The line');
});

test('Import YAML is checked against the schema first, then created with its problems (flow 7)', async ({ page }) => {
  const valid = `apiVersion: goal/v1
kind: Factory
metadata:
  id: 7a1f3c00-0000-4000-8000-0000000000aa
  name: imported-line
  title: Imported line
spec:
  agents:
    - name: importer
      prompt: ./agents/importer.md
  board:
    stations:
      - { id: inbox, kind: queue }
      - { id: work, kind: agent, agent: importer, wip: 1 }
      - { id: done, kind: done }
`;
  const invalid = valid.replace('kind: agent, agent: importer', 'kind: robot, agent: importer');
  const upload = (text: string) =>
    page
      .getByLabel('Upload a file')
      .setInputFiles({ name: 'factory.yaml', mimeType: 'text/yaml', buffer: Buffer.from(text) });

  await page.goto('/o/acme');
  if (!isPhone(page)) await page.getByRole('main').getByRole('link', { name: 'Import YAML' }).first().click();
  else await page.goto('/o/acme/new/start?from=yaml');
  await expect(page.getByRole('radio', { name: /^Import YAML/ })).toBeChecked();

  // A station kind the schema does not know: listed by line, and Continue stays disabled.
  await upload(invalid);
  const blocking = page.getByRole('list', { name: 'Fix these before you continue' });
  await expect(blocking).toContainText('kind must be queue, agent, human or done');
  await expect(blocking).toContainText(/Line \d+:/);
  await expect(continueTo(page, 'Basics')).toHaveAttribute('aria-disabled', 'true');
  await checkPage(page);

  // It parses and passes the schema; the missing prompt is for the next steps.
  await upload(valid);
  await expect(page.getByText('It parses and matches the schema.')).toBeVisible();
  await expect(page.getByRole('list', { name: 'You can fix these in the next steps' })).toContainText(
    'agents/importer.md not found',
  );
  await expect(continueTo(page, 'Basics')).not.toHaveAttribute('aria-disabled', 'true');
  await checkPage(page);

  await continueTo(page, 'Basics').click();
  await expect(page).toHaveURL(/\/o\/acme\/new\/basics\?factory=/);
  await expect(page.getByRole('heading', { level: 1, name: 'imported-line' })).toBeVisible();
  await expect(page.getByText('Imported from YAML.', { exact: false })).toBeVisible();

  // Under Agents, the problem waits on the agent.
  await page.goto(`/o/acme/new/agents?factory=${factoryId(page)}&agent=importer`);
  await expect(page.getByRole('heading', { level: 3, name: 'importer' })).toBeVisible();
  await expect(page.getByText('agents/importer.md not found', { exact: false })).toBeVisible();
  await checkPage(page);
});

test('a name another factory has is said on the name field', async ({ page }) => {
  await page.goto('/o/acme/new/start?from=yaml');
  const taken = `apiVersion: goal/v1
kind: Factory
metadata:
  id: 7a1f3c00-0000-4000-8000-0000000000bb
  name: goal
spec:
  agents:
    - name: one
  board:
    stations:
      - { id: inbox, kind: queue }
      - { id: done, kind: done }
`;
  await page
    .getByLabel('Upload a file')
    .setInputFiles({ name: 'factory.yaml', mimeType: 'text/yaml', buffer: Buffer.from(taken) });
  await expect(page.getByText('It parses and matches the schema.')).toBeVisible();
  const name = page.getByRole('textbox', { name: 'Name' });
  await expect(name).toHaveAccessibleDescription(/Acme already has a factory with factory\.yaml’s name/);
  await expect(continueTo(page, 'Basics')).toHaveAttribute('aria-disabled', 'true');

  await name.fill('goal-two');
  await continueTo(page, 'Basics').click();
  await expect(page.getByRole('heading', { level: 1, name: 'goal-two' })).toBeVisible();

  // goald still refuses a taken name when it creates (another tab took it): said on the field.
  await page.goto('/o/acme/new/start?from=blank');
  await page.getByRole('textbox', { name: 'Name' }).fill('sre');
  await continueTo(page, 'Basics').click();
  await expect(page.getByRole('textbox', { name: 'Name' })).toHaveAccessibleDescription(
    /Acme already has a factory named sre\. Choose another name\./,
  );
  await expect(page).toHaveURL(/\/new\/start\?from=blank$/);
});

test('a blank factory: save and exit, resume from the Factories home, delete it', async ({ page }) => {
  await page.goto('/o/acme/new/start?from=blank');
  await expect(page.getByRole('radio', { name: /^Blank/ })).toBeChecked();
  await expect(page.getByText('An empty line. Add stations next, then the agents who work at them.')).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Name' })).toHaveValue('new-factory');
  await continueTo(page, 'Basics').click();
  await expect(page.getByRole('heading', { level: 1, name: 'new-factory' })).toBeVisible();
  await expect(page.getByText('A blank factory.', { exact: false })).toBeVisible();
  const id = factoryId(page);

  await page.getByRole('textbox', { name: 'Title' }).fill('New factory');
  await continueTo(page, 'Line').click();
  await expect(page.getByRole('list', { name: 'Stations, in order' }).getByRole('listitem')).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Add station' })).toBeVisible();
  await checkPage(page);

  await page.getByRole('button', { name: 'Save and exit' }).click();
  await expect(page).toHaveURL(/\/o\/acme$/);
  const card = page.getByRole('article', { name: 'new-factory', exact: true });
  await expect(card.getByText('Draft', { exact: true })).toBeVisible();
  await checkPage(page);

  // Continue setup opens the first step not left with Continue: Line.
  if (isPhone(page)) await card.getByRole('link', { name: 'new-factory' }).click();
  else await card.getByRole('link', { name: 'Continue setup' }).click();
  await expect(page).toHaveURL(new RegExp(`/o/acme/new/line\\?factory=${id}$`));

  await page.getByRole('button', { name: 'Delete this draft factory…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Delete new-factory?' });
  await expect(dialog).toContainText('It was never published');
  await checkPage(page);
  await dialog.getByRole('button', { name: 'Delete draft factory' }).click();
  await expect(page).toHaveURL(/\/o\/acme$/);
  await expect(page.getByRole('article', { name: 'new-factory', exact: true })).toHaveCount(0);
});

test('Cancel on Start creates nothing; a clone starts from the running version', async ({ page }) => {
  await page.goto('/o/acme/new/start?from=clone');
  await page.getByRole('link', { name: 'Cancel' }).click();
  await expect(page).toHaveURL(/\/o\/acme$/);
  await expect(page.getByRole('article')).toHaveCount(3);

  await page.goto('/o/acme/new/start?from=clone');
  await expect(continueTo(page, 'Basics')).toHaveAttribute('aria-disabled', 'true');
  await page.getByRole('radio', { name: /^sre/ }).check();
  await expect(page.getByRole('textbox', { name: 'Name' })).toHaveValue('sre-copy');
  await checkPage(page);
  await continueTo(page, 'Basics').click();
  await expect(page.getByRole('heading', { level: 1, name: 'sre-copy' })).toBeVisible();
  await expect(page.getByText('A copy of sre v3.', { exact: false })).toBeVisible();
});
