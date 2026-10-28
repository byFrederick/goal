// The YAML view (D10) against the mock: goal has a draft of v9 at revision 42 based on v8 (3
// changes in 2 sections, 1 warning). Flows 2 and 3 of D §13, their YAML parts: Form edits show
// in factory.yaml with its comments, switching keeps the place, an unknown agent is a problem
// with its quick fix, a runtime size of xlarge is applied by its fix ("Use large"), and a
// factory.yaml that stops parsing holds Form until it parses again. Monaco on desktop and tablet, the textarea on phones.
import { expect, test } from '@playwright/test';
import { definition, editorReady, expectShown, monacoTimeout, openYaml, problems, replaceLine, status } from './editor';
import { expectAccessible, expectNoHorizontalScroll, isPhone } from './support';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

const editorView = (page: import('@playwright/test').Page) => page.getByRole('group', { name: 'Editor view' });

test('the YAML view: the tree marks changes, the outline lists the file, accessible at every width', async ({
  page,
}) => {
  await openYaml(page);
  await expect(editorView(page).getByRole('button', { name: 'YAML' })).toHaveAttribute('aria-pressed', 'true');
  await expect(page.getByRole('tab', { name: 'factory.yaml' })).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('button', { name: 'frontend-engineer.md, changed' })).toBeAttached();
  await expect(page.getByRole('complementary', { name: 'Outline' })).toContainText('runtime');
  await expect(problems(page)).toContainText('is asked as Off until tool approvals arrive');
  await expectAccessible(page);
  await expectShown(page, "# the spec's lane picks the engineer");
  await expectNoHorizontalScroll(page);

  // A file of a folder opens from the tree (agents/ is open at first); Form from there is that agent.
  await page.getByRole('button', { name: 'reviewer.md' }).click();
  await expect(page.getByRole('textbox', { name: 'agents/reviewer.md, draft of v9' })).toBeAttached();
  await editorView(page).getByRole('button', { name: 'Form' }).click();
  await expect(page).toHaveURL(/\/definition\/agents\?agent=reviewer$/);
  await expectAccessible(page);
});

test('Form ↔ YAML: one draft, comments kept, the place kept (flow 2)', async ({ page }) => {
  await page.goto(`${definition}/basics`);
  const title = page.getByRole('textbox', { name: 'Title' });
  await title.fill('Goal platform');
  await expect(status(page)).toContainText('Autosaved');

  await editorView(page).getByRole('button', { name: 'YAML' }).click();
  await editorReady(page);
  await expectShown(page, 'title: Goal platform');
  await expectShown(page, "# the spec's lane picks the engineer");

  // A check added in YAML is a change of the Line: the tree marks the file as changed.
  await replaceLine(
    page,
    '- { id: spec, name: Spec, kind: agent, agent: spec-writer, wip: 2 }',
    '- { id: spec, name: Spec, kind: agent, agent: spec-writer, wip: 2, checks: [go vet] }',
  );
  await expect(page.getByRole('button', { name: /^factory\.yaml.*changed/ })).toBeAttached();

  // Form opens at the station under the cursor.
  const form = editorView(page).getByRole('button', { name: 'Form' });
  await form.click();
  await expect(page).toHaveURL(/\/definition\/line\?station=spec$/);
  // The Spec row, with the check added in YAML, has focus.
  await expect(page.getByRole('textbox', { name: 'Name of station 2' })).toBeFocused();
  await expect(page.getByRole('listitem', { name: /^2\. Spec/ })).toContainText('go vet');
});

test('an unknown agent is a problem in place, with its quick fix (flow 3, YAML)', async ({ page }) => {
  await openYaml(page);
  await replaceLine(
    page,
    '- { id: spec, name: Spec, kind: agent, agent: spec-writer, wip: 2 }',
    '- { id: spec, name: Spec, kind: agent, agent: ghost, wip: 2 }',
  );

  await expect(problems(page)).toContainText('station spec: unknown agent "ghost"');
  await expect(problems(page)).toContainText(/factory\.yaml, line \d+/);
  if (!isPhone(page)) {
    await expect(page.locator('.monaco-editor .goal-glyph-problem').first()).toBeVisible({ timeout: monacoTimeout });
  }
  await expectAccessible(page);

  // The quick fix is offered under the line (the phone's Problems panel has it instead).
  await expect(page.getByRole('button', { name: 'Set agent to spec-writer' }).first()).toBeVisible();
});

test('a quick fix applies to the draft: xlarge is set to large (flow 3, YAML)', async ({ page }) => {
  await openYaml(page, 'basics');
  await replaceLine(page, 'size: large', 'size: xlarge');

  await expect(problems(page)).toContainText('spec.runtime.size: must be small, medium or large');
  await expect(problems(page)).toContainText(/factory\.yaml, line \d+/);
  if (!isPhone(page)) {
    await expect(page.locator('.monaco-editor .goal-glyph-problem').first()).toBeVisible({ timeout: monacoTimeout });
  }
  await expectAccessible(page);

  await page.getByRole('button', { name: 'Use large' }).first().click();
  await expectShown(page, 'size: large');
  await expect(problems(page)).not.toContainText('must be small, medium or large');
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('a quick fix applies in dark mode too (flow 3, YAML)', async ({ page }) => {
  await page.emulateMedia({ colorScheme: 'dark' });
  await openYaml(page, 'basics');
  await replaceLine(page, 'size: large', 'size: xlarge');
  await expect(problems(page)).toContainText('spec.runtime.size: must be small, medium or large');
  await expectAccessible(page);

  await page.getByRole('button', { name: 'Use large' }).first().click();
  await expectShown(page, 'size: large');
  await expect(problems(page)).not.toContainText('must be small, medium or large');
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('a factory.yaml that stops parsing holds Form until it parses again', async ({ page }) => {
  await openYaml(page, 'basics');
  await replaceLine(page, 'kind: Factory', 'kind: Factory: x');

  await expect(status(page)).toContainText('factory.yaml doesn’t parse');
  await expect(page.getByRole('button', { name: 'Format' })).toBeDisabled();

  await editorView(page).getByRole('button', { name: 'Form' }).click();
  const held = page.getByRole('alert').filter({ hasText: 'factory.yaml doesn’t parse' });
  await expect(held).toContainText(/has a syntax error on line \d+\. Visual opens again when it parses\./);
  await expect(page.getByRole('textbox', { name: 'Title' })).toHaveCount(0);
  await expectAccessible(page);

  await held.getByRole('link', { name: 'Open in YAML' }).click();
  await editorReady(page);
  await replaceLine(page, 'kind: Factory: x', 'kind: Factory');

  await editorView(page).getByRole('button', { name: 'Form' }).click();
  await expect(page.getByRole('textbox', { name: 'Title' })).toHaveValue('Goal');
  await expect(status(page)).not.toContainText('doesn’t parse');
});

test('a binary file of the draft downloads byte for byte', async ({ page, request }) => {
  const path = 'skills/acme-conventions/logo.png';
  const bytes = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x00, 0x01, 0xfe, 0xff]);
  const put = await request.put(`/v1/orgs/acme/factories/goal/draft/files/${encodeURIComponent(path)}`, {
    data: bytes,
    headers: { 'If-None-Match': '*', 'Content-Type': 'application/octet-stream' },
  });
  expect(put.ok()).toBe(true);

  await page.goto(`${definition}/line?mode=yaml&file=${encodeURIComponent(path)}`);
  await expect(page.getByText('Binary file, 8 bytes')).toBeVisible();
  const downloaded = page.waitForEvent('download');
  await page.getByRole('button', { name: 'Download' }).click();
  const file = await downloaded;

  expect(file.suggestedFilename()).toBe('logo.png');
  const stream = await file.createReadStream();
  const chunks: Buffer[] = [];
  for await (const chunk of stream) chunks.push(chunk as Buffer);
  expect([...Buffer.concat(chunks)]).toEqual([...bytes]);
});
