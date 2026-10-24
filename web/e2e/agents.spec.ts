// Definition › Agents (D8) against the mock: goal's draft of v9 has four agents; frontend-engineer
// asks before fetching web pages (B's builtin-ask-unavailable warning).
import { expect, type Page, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll, isPhone } from './support';

const agents = '/o/acme/f/goal/definition/agents';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

const status = (page: Page) => page.getByRole('status').filter({ hasText: /Based on|runs now/ });
const tile = (page: Page, name: RegExp) =>
  page.getByRole('navigation', { name: 'Definition sections' }).getByRole('link', { name });
const detail = (page: Page, name: string) => page.getByRole('region', { name, exact: true });
const prompt = (page: Page, agent: string) => page.getByRole('textbox', { name: `Prompt of ${agent}` });

/** Replaces the prompt's text: Monaco on a desktop, the textarea on a phone. */
async function typePrompt(page: Page, agent: string, text: string) {
  const editor = prompt(page, agent);
  await editor.focus();
  await page.keyboard.press('ControlOrMeta+A');
  await page.keyboard.press('Delete');
  await page.keyboard.type(text);
}

async function openAgent(page: Page, agent: string) {
  await page.goto(`${agents}?agent=${agent}`);
  await expect(page.getByRole('heading', { level: 3, name: agent })).toBeVisible();
}

test('the agents list says where each agent works and opens its detail', async ({ page }) => {
  await page.goto(agents);
  await expect(page).toHaveTitle('Agents · Definition · goal · Acme · Goal');
  const list = page.getByRole('navigation', { name: 'Agents' });
  await expect(list.getByRole('heading', { name: '4 agents' })).toBeVisible();
  await expect(list.getByRole('link', { name: /backend-engineer/ })).toContainText('Build, backend lane');
  await expect(list.getByRole('link', { name: /reviewer/ })).toContainText('Review, gate');
  if (isPhone(page)) {
    // The list comes first on a phone; an agent opens its detail with a way back.
    await expect(page.getByRole('heading', { level: 3, name: 'spec-writer' })).toBeHidden();
  } else {
    await expect(list.getByRole('link', { name: /spec-writer/ })).toHaveAttribute('aria-current', 'true');
  }
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);

  await list.getByRole('link', { name: /reviewer/ }).click();
  await expect(page).toHaveURL(/\?agent=reviewer$/);
  await expect(page.getByRole('heading', { level: 3, name: 'reviewer' })).toBeVisible();
  await expect(page.getByText('Works at Review, gate.')).toBeVisible();
  if (isPhone(page)) {
    await expect(list).toBeHidden();
    await page.getByRole('link', { name: 'All agents' }).click();
    await expect(list).toBeVisible();
    await list.getByRole('link', { name: /reviewer/ }).click();
  }
  await page.getByRole('link', { name: 'Open agent page' }).click();
  await expect(page).toHaveURL(/\/o\/acme\/f\/goal\/agents\/reviewer$/);
  await expect(page.getByRole('heading', { level: 1, name: 'reviewer' })).toBeVisible();
});

test('model, prompt, a skills.sh skill and built-in tools save with the draft', async ({ page }) => {
  await openAgent(page, 'frontend-engineer');
  const agent = detail(page, 'frontend-engineer');

  await agent.getByRole('combobox', { name: 'Model' }).selectOption('claude-opus-5-5');

  await expect(prompt(page, 'frontend-engineer')).toBeVisible();
  await typePrompt(page, 'frontend-engineer', '# frontend-engineer\n\nBuild the web change the spec asks for.');
  await expect(agent.getByText('Markdown, 10 words')).toBeVisible();
  await agent.getByRole('radio', { name: 'Preview' }).check();
  await expect(agent.getByRole('heading', { level: 5, name: 'frontend-engineer' })).toBeVisible();
  await expect(agent.getByText('Build the web change the spec asks for.')).toBeVisible();
  await agent.getByRole('radio', { name: 'Write' }).check();

  // A skills.sh skill: search, read its SKILL.md, add it.
  const picker = agent.getByRole('combobox', { name: 'Add a skill from the catalog' });
  await picker.fill('terraform');
  await page.getByRole('option', { name: /terraform-style-guide/ }).click();
  const dialog = page.getByRole('dialog', { name: 'Add terraform-style-guide' });
  await expect(dialog).toContainText('acme/agent-skills, pinned at 3f1c2a9, 2 files');
  await expect(dialog.getByRole('region', { name: 'SKILL.md' })).toContainText('Name resources in snake_case.');
  await expect(dialog).toContainText('Review it before you publish.');
  await expectAccessible(page);
  await dialog.getByRole('button', { name: 'Add skill' }).click();
  await expect(dialog).toBeHidden();
  await expect(agent.getByRole('button', { name: 'Remove terraform-style-guide' })).toBeVisible();

  // Built-in tools: Ask me warns until tool approvals arrive; Allowed clears it.
  const web = agent.getByRole('group', { name: 'Fetch web pages' });
  await expect(web.getByRole('radio', { name: 'Ask me' })).toBeChecked();
  await expect(agent.getByText('Asked as Off until tool approvals arrive')).toBeVisible();
  await web.getByRole('radio', { name: 'Allowed' }).check();
  await expect(agent.getByText('Asked as Off until tool approvals arrive')).toBeHidden();

  await expect(status(page)).toContainText('Autosaved');
  await expect(tile(page, /^Agents, \d+ changes$/)).toBeAttached();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);

  await page.reload();
  const again = detail(page, 'frontend-engineer');
  await expect(again.getByRole('combobox', { name: 'Model' })).toHaveValue('claude-opus-5-5');
  await expect(again.getByRole('button', { name: 'Remove terraform-style-guide' })).toBeVisible();
  await expect(
    again.getByRole('group', { name: 'Fetch web pages' }).getByRole('radio', { name: 'Allowed' }),
  ).toBeChecked();
  await expect(again.getByText('Markdown, 10 words')).toBeVisible();
});

test('the agent page’s link opens the skill picker focused', async ({ page }) => {
  await page.goto(`${agents}?agent=reviewer&add=skill`);
  const picker = page.getByRole('combobox', { name: 'Add a skill from the catalog' });
  await expect(picker).toBeFocused();
  await expect(page.getByRole('listbox', { name: 'Skills' })).toBeVisible();
  // The factory's own skill comes first, then the catalog.
  await expect(page.getByRole('option', { name: 'acme-conventions' })).toHaveAttribute('aria-selected', 'true');
  await picker.press('Enter');
  await expect(page.getByRole('button', { name: 'Remove acme-conventions' })).toBeVisible();
});

test('undoing a rename moves the prompt back', async ({ page }) => {
  await openAgent(page, 'frontend-engineer');
  await page.getByRole('button', { name: 'Rename…' }).click();
  const rename = page.getByRole('dialog', { name: 'Rename frontend-engineer' });
  await rename.getByRole('textbox', { name: 'Name' }).fill('web-engineer');
  await rename.getByRole('button', { name: 'Rename' }).click();
  await expect(page.getByRole('heading', { level: 3, name: 'web-engineer' })).toBeVisible();
  await expect(status(page)).toContainText('Autosaved');

  await page.keyboard.press('ControlOrMeta+Z');
  await expect(status(page)).toContainText('Saving');
  await expect(status(page)).toContainText('Autosaved');
  await openAgent(page, 'frontend-engineer');
  await expect(prompt(page, 'frontend-engineer')).toBeVisible();
  await expect(page.getByText('agents/frontend-engineer.md', { exact: true })).toBeVisible();
  await expect(page.getByText(/isn’t in the draft/)).toHaveCount(0);
});

test('removing an agent reads its prompt first: undo puts back one not loaded yet, a failed read removes nothing', async ({
  page,
  request,
}) => {
  await page.goto(agents);
  await page.getByRole('button', { name: 'Add agent' }).click();
  const add = page.getByRole('dialog', { name: 'Add agent' });
  await add.getByRole('textbox', { name: 'Name' }).fill('docs-writer');
  await add.getByRole('button', { name: 'Add agent' }).click();
  await expect(prompt(page, 'docs-writer')).toBeVisible();
  await expect(status(page)).toContainText('Autosaved');

  // goald can't give the prompt: nothing is removed.
  await request.post('/__mock/fail', { data: { path: 'docs-writer.md', method: 'GET', status: 503, times: 0 } });
  await page.reload();
  await page.getByRole('button', { name: 'Remove…' }).click();
  const confirm = page.getByRole('dialog', { name: 'Remove docs-writer?' });
  await confirm.getByRole('button', { name: 'Remove agent' }).click();
  await expect(confirm.getByRole('alert')).toHaveText(
    'The prompt could not be read, so nothing was removed. Try again.',
  );
  await confirm.getByRole('button', { name: 'Cancel' }).click();
  await expect(page.getByRole('heading', { level: 3, name: 'docs-writer' })).toBeVisible();
  await request.delete('/__mock/fail');

  // A fresh editor whose read of the prompt is still on its way when the agent is removed.
  await request.post('/__mock/fail', { data: { path: 'docs-writer.md', method: 'GET', delay_ms: 3000 } });
  await page.reload();
  await expect(page.getByRole('heading', { level: 3, name: 'docs-writer' })).toBeVisible();
  await page.getByRole('button', { name: 'Remove…' }).click();
  await confirm.getByRole('button', { name: 'Remove agent' }).click();
  const list = page.getByRole('navigation', { name: 'Agents' });
  await expect(list.getByRole('heading', { name: '4 agents' })).toBeVisible();
  await expect(status(page)).toContainText('Autosaved');

  await page.keyboard.press('ControlOrMeta+Z');
  await expect(list.getByRole('heading', { name: '5 agents' })).toBeVisible();
  await expect(status(page)).toContainText('Saving');
  await expect(status(page)).toContainText('Autosaved');
  await openAgent(page, 'docs-writer');
  await expect(prompt(page, 'docs-writer')).toBeVisible();
  await expect(detail(page, 'docs-writer').getByText('Markdown, 2 words')).toBeVisible();
  await expect(page.getByText(/isn’t in the draft/)).toHaveCount(0);
});

test('add, rename and remove an agent; a station keeps its agent from being removed', async ({ page, request }) => {
  await page.goto(agents);
  await page.getByRole('button', { name: 'Add agent' }).click();
  const add = page.getByRole('dialog', { name: 'Add agent' });
  await add.getByRole('textbox', { name: 'Name' }).fill('Docs');
  await add.getByRole('button', { name: 'Add agent' }).click();
  await expect(add.getByText(/Lowercase letters, digits and dashes/)).toBeVisible();
  await add.getByRole('textbox', { name: 'Name' }).fill('docs-writer');
  await add.getByRole('button', { name: 'Add agent' }).click();
  await expect(page).toHaveURL(/\?agent=docs-writer$/);
  await expect(page.getByRole('heading', { level: 3, name: 'docs-writer' })).toBeVisible();
  await expect(page.getByText('Not on the line yet: give it a station in Line.')).toBeVisible();
  await expect(prompt(page, 'docs-writer')).toBeVisible();

  // goald takes its time over the save that moves the prompt: the moved prompt shows meanwhile.
  await request.post('/__mock/fail', { data: { path: '/draft/model/agents', method: 'PUT', delay_ms: 3000 } });
  await page.getByRole('button', { name: 'Rename…' }).click();
  const rename = page.getByRole('dialog', { name: 'Rename docs-writer' });
  await rename.getByRole('textbox', { name: 'Name' }).fill('writer');
  await rename.getByRole('button', { name: 'Rename' }).click();
  await expect(page).toHaveURL(/\?agent=writer$/);
  await expect(page.getByRole('heading', { level: 3, name: 'writer' })).toBeVisible();
  await expect(prompt(page, 'writer')).toBeVisible();
  await expect(page.getByText('agents/writer.md', { exact: true })).toBeVisible();
  await expect(status(page)).toContainText('Saving');
  await expect(status(page)).toContainText('Autosaved');
  await expect(prompt(page, 'writer')).toBeVisible();

  await page.getByRole('button', { name: 'Remove…' }).click();
  const remove = page.getByRole('dialog', { name: 'Remove writer?' });
  await remove.getByRole('button', { name: 'Remove agent' }).click();
  const list = page.getByRole('navigation', { name: 'Agents' });
  await expect(list.getByRole('heading', { name: '4 agents' })).toBeVisible();
  await expect(list.getByRole('link', { name: /^writer / })).toHaveCount(0);

  await page.goto(`${agents}?agent=reviewer`);
  await page.getByRole('button', { name: 'Remove…' }).click();
  const blocked = page.getByRole('dialog', { name: 'reviewer can’t be removed yet' });
  await expect(blocked.getByRole('link', { name: 'Review' })).toBeVisible();
  await expectAccessible(page);
  await blocked.getByRole('link', { name: 'Review' }).click();
  await expect(page).toHaveURL(/\/definition\/line\?station=review$/);
});
