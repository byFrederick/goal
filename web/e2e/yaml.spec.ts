// The YAML view (D10) against the mock: goal has a draft of v9 at revision 42 based on v8 (3
// changes in 2 sections, 1 warning). Flows 2 and 3 of D §13, their YAML parts: Visual edits show
// in factory.yaml with its comments, YAML edits count on the tiles, switching keeps the place, an
// unknown agent is a problem with a fix, and a factory.yaml that stops parsing holds Visual until
// it parses again. Monaco on desktop and tablet, the textarea on phones.
import { expect, test } from '@playwright/test';
import {
  definition,
  editorReady,
  expectShown,
  monacoTimeout,
  openYaml,
  problems,
  replaceLine,
  status,
  tile,
} from './editor';
import { expectAccessible, expectNoHorizontalScroll, isPhone } from './support';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

test('the YAML view: tiles jump to blocks, problems and changes beside, accessible at every width', async ({
  page,
}) => {
  await openYaml(page);
  const toggle = page.getByRole('navigation', { name: 'Editor view' });
  await expect(toggle.getByRole('link', { name: 'YAML' })).toHaveAttribute('aria-current', 'page');
  await expect(tile(page, /^Line, board, lines \d+–\d+, 2 changes$/)).toHaveAttribute('aria-current', 'location');
  await expect(tile(page, /^Agents, agents, lines \d+–\d+, 1 change, 1 problem$/)).toBeAttached();
  await expect(page.getByRole('tab', { name: 'factory.yaml' })).toHaveAttribute('aria-selected', 'true');
  await expect(problems(page)).toContainText('is asked as Off until tool approvals arrive');
  await expect(page.getByRole('region', { name: 'Changes from v8' })).toContainText('Line: Build');
  await expectAccessible(page);
  await expectShown(page, "# the spec's lane picks the engineer");
  await expectNoHorizontalScroll(page);

  // A tile in YAML mode jumps to its block: the cursor goes to its first line (?line=).
  await tile(page, /^Basics, metadata/).click();
  await expect(page).toHaveURL(/\/definition\/basics\?mode=yaml&line=\d+/);

  // An agent's prompt opens from the agents/ tab; Visual from there is that agent.
  await page.getByRole('tab', { name: /^agents\/ \(4\)/ }).click();
  await page.getByRole('menuitem', { name: 'agents/reviewer.md' }).click();
  await expect(page.getByRole('textbox', { name: 'agents/reviewer.md, draft of v9' })).toBeAttached();
  await expect(toggle.getByRole('link', { name: 'Visual' })).toHaveAttribute(
    'href',
    /\/definition\/agents\?agent=reviewer$/,
  );
  await expectAccessible(page);
});

test('Visual ↔ YAML: one draft, comments kept, the place kept (flow 2)', async ({ page }) => {
  await page.goto(`${definition}/basics`);
  const title = page.getByRole('textbox', { name: 'Title' });
  await title.fill('Goal platform');
  await expect(status(page)).toContainText('Autosaved');

  await page.getByRole('navigation', { name: 'Editor view' }).getByRole('link', { name: 'YAML' }).click();
  await editorReady(page);
  await expectShown(page, 'title: Goal platform');
  await expectShown(page, "# the spec's lane picks the engineer");

  // A check added in YAML is a change of the Line.
  await replaceLine(
    page,
    '- { id: spec, name: Spec, kind: agent, agent: spec-writer, wip: 2 }',
    '- { id: spec, name: Spec, kind: agent, agent: spec-writer, wip: 2, checks: [go vet] }',
  );
  await expect(tile(page, /^Line, board, lines \d+–\d+, 3 changes$/)).toBeAttached();

  // Visual opens at the station under the cursor.
  const visual = page.getByRole('navigation', { name: 'Editor view' }).getByRole('link', { name: 'Visual' });
  await expect(visual).toHaveAttribute('href', /\/definition\/line\?station=spec$/);
  await visual.click();
  await expect(page).toHaveURL(/\/definition\/line\?station=spec$/);
  await expect(tile(page, /^Line, 3 changes$/)).toBeAttached();
  // The Spec row, with the check added in YAML, has focus.
  await expect(page.getByRole('textbox', { name: 'Name of station 2' })).toBeFocused();
  await expect(page.getByRole('listitem', { name: /^2\. Spec/ })).toContainText('go vet');
});

test('an unknown agent is a problem in place, with a fix (flow 3, YAML)', async ({ page }) => {
  await openYaml(page);
  await replaceLine(
    page,
    '- { id: spec, name: Spec, kind: agent, agent: spec-writer, wip: 2 }',
    '- { id: spec, name: Spec, kind: agent, agent: ghost, wip: 2 }',
  );

  await expect(problems(page)).toContainText('station spec: unknown agent "ghost"');
  await expect(problems(page)).toContainText(/Line \d+, line › spec/);
  if (!isPhone(page)) {
    await expect(page.locator('.monaco-editor .goal-glyph-problem').first()).toBeVisible({ timeout: monacoTimeout });
  }
  await expect(tile(page, /^Line, board, lines \d+–\d+, 3 changes, 1 problem$/)).toBeAttached();
  await expect(tile(page, /^Review and publish, budget, .*Fix 2 problems first$/)).toBeAttached();
  await expectAccessible(page);

  await problems(page).getByRole('button', { name: 'Set agent to spec-writer' }).click();
  await expect(problems(page)).not.toContainText('ghost');
  await expectShown(page, 'agent: spec-writer, wip: 2 }');
  await expect(tile(page, /^Line, board, lines \d+–\d+, 2 changes$/)).toBeAttached();
});

test('a factory.yaml that stops parsing holds Visual until it parses again', async ({ page }) => {
  await openYaml(page, 'basics');
  await replaceLine(page, 'kind: Factory', 'kind: Factory: x');

  await expect(tile(page, /^Basics, metadata, .*Syntax error, line \d+$/)).toBeAttached();
  await expect(page.getByRole('button', { name: 'Format' })).toBeDisabled();
  await expect(status(page)).toContainText('factory.yaml doesn’t parse');

  await page.getByRole('navigation', { name: 'Editor view' }).getByRole('link', { name: 'Visual' }).click();
  const held = page.getByRole('alert').filter({ hasText: 'factory.yaml doesn’t parse' });
  await expect(held).toContainText(/has a syntax error on line \d+\. Visual opens again when it parses\./);
  await expect(page.getByRole('textbox', { name: 'Title' })).toHaveCount(0);
  await expectAccessible(page);

  await held.getByRole('link', { name: 'Open in YAML' }).click();
  await editorReady(page);
  await replaceLine(page, 'kind: Factory: x', 'kind: Factory');
  await expect(tile(page, /^Basics, metadata, lines \d+–\d+, No changes$/)).toBeAttached();

  await page.getByRole('navigation', { name: 'Editor view' }).getByRole('link', { name: 'Visual' }).click();
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
