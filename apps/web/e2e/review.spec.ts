// Review and publish, Versions (D11) against the mock: goal runs v8 and has a draft of v9 (3
// changes in 2 sections, 1 warning). Flows 3 and 5 of D §13: a problem blocks Publish until it
// is fixed, then v9 runs; Compare v7, Restore v7, Discard the draft. And a station the draft
// removes while items wait there asks where they go before the publish.
import { expect, type Page, test } from '@playwright/test';
import { definition, openYaml, problems, replaceLine, status } from './editor';
import { expectAccessible, expectNoHorizontalScroll, expectToast, recordToasts } from './support';

test.beforeEach(async ({ page, request }) => {
  await request.post('/__mock/reset');
  await recordToasts(page);
});

const reviewLink = (page: Page) => page.getByRole('link', { name: 'Review and publish', exact: true });
const publishButton = (page: Page) => page.getByRole('button', { name: /^Publish v\d+$/ });
const history = (page: Page) => page.getByRole('region', { name: 'Versions' });
const versionRow = (page: Page, n: number) =>
  history(page)
    .getByRole('listitem')
    .filter({ has: page.getByText(`v${n}`, { exact: true }) });

async function openReview(page: Page, search = '') {
  await page.goto(`${definition}/review${search}`);
  await expect(page.getByRole('region', { name: 'Checks' })).toBeVisible();
}

test('Review is accessible and fits the screen; the copy says runs finish on v8', async ({ page }) => {
  await openReview(page);
  await expect(page.getByRole('region', { name: 'Checks' })).toContainText('passed, 1 problem');
  await expect(page.getByRole('heading', { name: 'Publish v9' })).toBeVisible();
  await expect(page.getByText('3 changes', { exact: true })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Changes to factory.yaml' })).toContainText('@@ Line: build station');
  await expect(publishButton(page)).toHaveText('Publish v9');
  await expect(page.getByRole('button', { name: 'Evals before publish' })).toHaveAttribute('aria-disabled', 'true');
  await expect(page.getByRole('region', { name: 'Publish' })).toContainText(
    'You can roll back to any earlier version later.',
  );
  await expect(page.getByText('What happens to work in progress')).toBeVisible();
  await expect(versionRow(page, 8)).toContainText('Live');
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('a problem blocks Publish until it is fixed; then v9 runs (flow 3)', async ({ page }) => {
  await openYaml(page);
  await replaceLine(
    page,
    '- { id: spec, name: Spec, kind: agent, agent: spec-writer, wip: 2 }',
    '- { id: spec, name: Spec, kind: agent, agent: ghost, wip: 2 }',
  );
  await expect(problems(page)).toContainText('unknown agent "ghost"');

  await reviewLink(page).click();
  const alert = page.getByRole('alert').filter({ hasText: 'Fix 1 problem before you publish v9' });
  await expect(alert).toContainText('Line: station spec: unknown agent "ghost"');
  await expect(publishButton(page)).toHaveAttribute('aria-disabled', 'true');
  await expect(publishButton(page)).toHaveAccessibleDescription('Fix 1 problem first. Your draft stays saved.');
  await expectAccessible(page);

  // The fix button in YAML puts the agent back.
  await page.getByRole('navigation', { name: 'Editor view' }).getByRole('link', { name: 'YAML' }).click();
  await problems(page).getByRole('button', { name: 'Set agent to spec-writer' }).click();
  await expect(problems(page)).not.toContainText('ghost');

  await reviewLink(page).click();
  await expect(alert).toHaveCount(0);
  await expect(publishButton(page)).not.toHaveAttribute('aria-disabled');
  await page.getByRole('textbox', { name: 'What changed, in one line' }).fill('Build also runs pnpm test');
  await publishButton(page).click();

  await expectToast(page, 'goal v9 published');
  await expect(page.getByRole('region', { name: 'Publish' })).toContainText('No draft. v9 is running.');
  await expect(versionRow(page, 9)).toContainText('Build also runs pnpm test');
  await expect(versionRow(page, 9)).toContainText('Live');
  await expect(status(page)).toContainText('v9 runs now.');
});

test('after a publish the editor reads no draft that is gone', async ({ page }) => {
  await openReview(page);
  const failed: string[] = [];
  page.on('console', (m) => {
    if (m.type() === 'error') failed.push(m.text());
  });
  page.on('response', (r) => {
    if (r.status() >= 400) failed.push(`${r.status()} ${new URL(r.url()).pathname}`);
  });

  await publishButton(page).click();
  await expectToast(page, 'goal v9 published');
  await expect(page.getByRole('region', { name: 'Publish' })).toContainText('No draft. v9 is running.');
  await page
    .getByRole('link', { name: /^Line, / })
    .first()
    .click();
  await expect(status(page)).toContainText('v9 runs now.');
  expect(failed).toEqual([]);
});

test('Compare v7, Restore v7, then discard the draft (flow 5)', async ({ page }) => {
  await openReview(page);

  await versionRow(page, 7).getByRole('button', { name: 'Compare v7' }).click();
  await expect(page).toHaveURL(/\?compare=7\.\.draft$/);
  await expect(versionRow(page, 7).getByRole('button', { name: 'Comparing v7' })).toHaveAttribute(
    'aria-pressed',
    'true',
  );
  await expect(page.getByRole('region', { name: 'Changes from v7' })).toContainText('action: merge');

  await versionRow(page, 7).getByRole('button', { name: 'Restore v7…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Make v7 the running version?' });
  await expect(dialog).toContainText('v8 stops taking new runs; your draft stays open.');
  await expectAccessible(page);
  await dialog.getByRole('button', { name: 'Restore v7' }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByText('v7 is running', { exact: true })).toBeVisible();
  await expect(versionRow(page, 7)).toContainText('Live');
  await expect(status(page)).toContainText('Based on v8; v7 is running.');

  await page.getByRole('button', { name: 'Discard draft…' }).click();
  const discard = page.getByRole('dialog', { name: 'Discard the draft of v9?' });
  await discard.getByRole('button', { name: 'Discard draft' }).click();
  await expect(status(page)).toContainText('v7 runs now. Your first change opens a draft.');
  await expect(page.getByRole('region', { name: 'Publish' })).toContainText('No draft. v7 is running.');

  // Versions lists every version; Compare without a draft is the version before it.
  await page.getByRole('link', { name: /^All \d+ versions$/ }).click();
  await expect(page.getByRole('heading', { level: 2, name: 'Versions' })).toBeFocused();
  await page.getByRole('button', { name: 'Compare v8' }).click();
  const v8Changes = page.getByRole('region', { name: 'Changes from v7 to v8' });
  await expect(v8Changes.getByRole('listitem').first()).toBeVisible();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('items waiting at a station the draft removes ask where they go', async ({ page }) => {
  await openYaml(page);
  await replaceLine(page, '- { id: inbox, name: Inbox, kind: queue }', '');
  await reviewLink(page).click();

  await expect(page.getByRole('alert').filter({ hasText: 'removes station inbox' })).toContainText(
    'removes station inbox, where 2 items are waiting',
  );
  await expect(publishButton(page)).toHaveAttribute('aria-disabled', 'true');
  await page.getByRole('combobox', { name: 'Items waiting at inbox, which v9 removes' }).selectOption('spec');
  await expect(publishButton(page)).not.toHaveAttribute('aria-disabled');
  await expect(page.getByRole('region', { name: 'Checks' })).toContainText(
    'No open items wait at a station it removes',
  );
  await publishButton(page).click();
  await expectToast(page, 'goal v9 published');
});
