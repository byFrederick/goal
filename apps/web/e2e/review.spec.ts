// Review and publish, Versions (D11) against the mock: goal runs v8 and has a draft of v9 (3
// changes in 2 sections, 1 warning). Flows 3 and 5 of D §13: a problem blocks Publish until it
// is fixed, then v9 runs; Compare v7, Restore v7, Discard the draft. And a station the draft
// removes while items wait there asks where they go before the publish.
import { expect, type Page, test } from '@playwright/test';
import { definition, openYaml, problems, replaceLine, status } from './editor';
import { expectAccessible, expectNoHorizontalScroll, expectToast, expectViewLoaded, recordToasts } from './support';

test.beforeEach(async ({ page, request }) => {
  await request.post('/__mock/reset');
  await recordToasts(page);
});

const reviewLink = (page: Page) => page.getByRole('link', { name: 'Review changes', exact: true }).first();
const publishButton = (page: Page) => page.getByRole('button', { name: /^Publish v\d+$/ });
const history = (page: Page) => page.getByRole('region', { name: 'Versions' });
const versionRow = (page: Page, n: number) =>
  history(page)
    .getByRole('listitem')
    .filter({ has: page.getByText(`v${n}`, { exact: true }) });

/**
 * Waits for what loads or fades in to settle. The kept tests cross-fade in from their skeleton, and axe reads
 * text that is half faded as low contrast; a scan of the settled page is the one a person sees.
 */
async function settled(page: Page) {
  await expect(page.locator('[aria-busy="true"]')).toHaveCount(0);
  await page.evaluate(() =>
    Promise.race([
      Promise.all(document.getAnimations().map((a) => a.finished.catch(() => null))),
      new Promise((resolve) => setTimeout(resolve, 3000)),
    ]),
  );
}

async function openReview(page: Page, search = '') {
  await page.goto(`${definition}/review${search}`);
  await expectViewLoaded(page);
  await expect(page.getByRole('region', { name: 'Checks' })).toBeVisible();
}

test('Review is accessible and fits the screen; the copy says runs finish on v8', async ({ page }) => {
  await openReview(page);
  await expect(page.getByRole('region', { name: 'Checks' })).toContainText('passed, 2 problems');
  await expect(page.getByRole('heading', { name: 'Publish v9' })).toBeVisible();
  await expect(page.getByText('3 changes', { exact: true })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Changes to factory.yaml' })).toContainText('@@ Line: build station');
  await expect(publishButton(page)).toHaveText('Publish v9');
  await expect(page.getByRole('region', { name: 'Publish' })).toContainText(
    'You can roll back to any earlier version later.',
  );
  await expect(page.getByText('What happens to work in progress')).toBeVisible();
  await expect(versionRow(page, 8)).toContainText('Live');
  await settled(page);
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
  await settled(page);
  await expectAccessible(page);

  // The fix button in the Line section puts the agent back. (The YAML editor offers it too, under the line;
  // yaml.spec.ts covers that.)
  await page
    .getByRole('link', { name: /^Line, / })
    .first()
    .click();
  await page.getByRole('button', { name: 'Set agent to spec-writer' }).first().click();
  await expect(page.getByRole('combobox', { name: 'Who works at Spec' })).toHaveText(/spec-writer/);

  await reviewLink(page).click();
  await expect(alert).toHaveCount(0);
  await expect(publishButton(page)).not.toHaveAttribute('aria-disabled');
  await page.getByRole('textbox', { name: 'What changed, in one line' }).fill('Build also runs pnpm test');
  await publishButton(page).click();

  await expectToast(page, 'goal v9 published');
  // The card keeps the publish's check until the page is left: "v9 is published and running" (MOTION.md, publish succeeds).
  await expect(page.getByRole('region', { name: 'Publish' })).toContainText('v9 is published and running.');
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
  await expect(page.getByRole('region', { name: 'Publish' })).toContainText('v9 is published and running.');
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
  await settled(page);
  await expectAccessible(page);
  await dialog.getByRole('button', { name: 'Restore v7' }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByText('v7 is running', { exact: true })).toBeVisible();
  await expect(versionRow(page, 7)).toContainText('Live');
  await expect(status(page)).toContainText('Based on v8; v7 is running.');

  await page.getByRole('button', { name: 'Discard draft', exact: true }).click();
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
  await settled(page);
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('items waiting at a station the draft removes ask where they go', async ({ page }) => {
  await openYaml(page);
  await replaceLine(page, '- { id: inbox, name: Inbox, kind: queue }', '');
  // The check runs on the saved draft: leave only once it has read the removal.
  await expect(problems(page)).toContainText('removes station inbox');
  await reviewLink(page).click();

  await expect(page.getByRole('alert').filter({ hasText: 'removes station inbox' })).toContainText(
    'removes station inbox, where 3 items are waiting',
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

test('Review lists the draft’s kept tests with their results, and Versions shows the watch chips', async ({ page }) => {
  await openReview(page);
  const kept = page.getByRole('region', { name: 'Publish' }).locator('section[aria-labelledby="kept-tests-h"]');
  await expect(kept.getByRole('heading', { name: 'Kept tests' })).toBeVisible();
  const build = kept.getByRole('listitem').filter({ hasText: 'Build' });
  await expect(build.getByText('Passed')).toBeVisible();
  await expect(build.getByText('Failed')).toBeVisible();
  // Review lists the last four versions: v8 is watched, v6 helped and v5 changed nothing. The one that got worse
  // (v4, mock/jonah.ts goalWatches) is on the full list.
  for (const chip of ['Helped', 'No change', 'Watching']) {
    await expect(history(page).getByText(chip, { exact: true }).first()).toBeVisible();
  }
  await page.getByRole('link', { name: /^All \d+ versions$/ }).click();
  await expect(page.getByText('Worse', { exact: true }).first()).toBeVisible();
});
