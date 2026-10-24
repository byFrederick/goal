// The Line section (D7) against the mock: goal's draft of v9 has Inbox, Spec, Approve spec,
// Build, Review, Merge (a merge step) and Done; sre has no repository and no draft.
import { expect, type Locator, type Page, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll } from './support';

const goalLine = '/o/acme/f/goal/definition/line';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

const stations = (page: Page) => page.getByRole('list', { name: 'Stations, in order' });
const rows = (page: Page) => stations(page).locator(':scope > li');
const row = (page: Page, name: string) =>
  stations(page).getByRole('listitem', { name: new RegExp(`^\\d+\\. ${name},`) });
const status = (page: Page) => page.getByRole('status').filter({ hasText: /Based on|runs now/ });

/** The stations' names, in order. */
async function order(page: Page): Promise<string[]> {
  const labels = await rows(page).evaluateAll((lis) => lis.map((li) => li.getAttribute('aria-label') ?? ''));
  return labels.map((l) => l.replace(/^\d+\. /, '').replace(/, \w+ station$/, ''));
}

async function openLine(page: Page, url = goalLine) {
  await page.goto(url);
  await expect(page.getByRole('heading', { level: 2, name: 'Line', exact: true })).toBeVisible();
  await expect(rows(page).first()).toBeVisible();
}

/** Drags a row by its number onto another row, with the mouse. */
async function drag(page: Page, from: Locator, to: Locator) {
  const handle = from.getByRole('button', { name: /^Drag / });
  await handle.scrollIntoViewIfNeeded();
  const a = await handle.boundingBox();
  if (!a) throw new Error('no handle');
  await page.mouse.move(a.x + a.width / 2, a.y + a.height / 2);
  await page.mouse.down();
  await page.mouse.move(a.x + a.width / 2, a.y + a.height / 2 + 12, { steps: 4 });
  const b = await to.boundingBox();
  if (!b) throw new Error('no target');
  await page.mouse.move(b.x + 40, b.y + b.height / 2, { steps: 12 });
  await page.mouse.up();
}

test('the line is accessible and fits the screen', async ({ page }) => {
  await openLine(page);
  await expect(page).toHaveTitle('Line · Definition · goal · Acme · Goal');
  expect(await order(page)).toEqual(['Inbox', 'Spec', 'Approve spec', 'Build', 'Review', 'Merge', 'Done']);
  await expect(row(page, 'Build')).toHaveAccessibleName('4. Build, agent station');
  await expect(row(page, 'Build').getByRole('radio', { name: 'Agent' })).toBeChecked();
  await expect(row(page, 'Build').getByText('Constraint', { exact: false })).toBeVisible();
  await expect(row(page, 'Build').getByRole('combobox', { name: 'Who works at Build' })).toHaveValue('__route');
  await expect(row(page, 'Review').getByRole('combobox', { name: 'If rejected, send back to' })).toHaveValue('build');
  await expect(row(page, 'Merge').getByRole('switch', { name: 'Merge step at Merge' })).toHaveAttribute(
    'aria-checked',
    'true',
  );
  await expect(row(page, 'Done').getByRole('radio', { name: 'Done' })).toBeChecked();
  await expect(row(page, 'Spec').getByRole('radio', { name: 'Done' })).toBeDisabled();
  await expect(page.getByRole('list', { name: 'Line preview' }).getByRole('listitem')).toHaveCount(7);
  await expect(page.getByText('Rework: Review sends work back to Build, at most twice.')).toBeVisible();
  await expect(page.getByRole('heading', { level: 2, name: 'Checks on this line' })).toBeVisible();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);

  // Flow and targets opens under the preview.
  await page.getByText('Flow and targets').click();
  await expect(page.getByRole('combobox', { name: 'Constraint' })).toHaveValue('build');
  await expect(page.getByRole('textbox', { name: 'Andon at rework rate' })).toHaveValue('40');
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('reorder by button, Alt+Arrow and drag; Done stays last', async ({ page }) => {
  await openLine(page);

  // 1. The ↑ button: focus stays on it in the moved row, and the move is announced.
  const up = row(page, 'Review').getByRole('button', { name: 'Move Review up' });
  await up.click();
  expect(await order(page)).toEqual(['Inbox', 'Spec', 'Approve spec', 'Review', 'Build', 'Merge', 'Done']);
  await expect(page.getByRole('status').filter({ hasText: 'Review moved to position 4 of 7' })).toBeAttached();
  await expect(row(page, 'Review').getByRole('button', { name: 'Move Review up' })).toBeFocused();

  // 2. Alt+ArrowDown from the name field puts it back; the field keeps focus.
  const name = row(page, 'Review').getByRole('textbox', { name: /^Name of station/ });
  await name.focus();
  await page.keyboard.press('Alt+ArrowDown');
  expect(await order(page)).toEqual(['Inbox', 'Spec', 'Approve spec', 'Build', 'Review', 'Merge', 'Done']);
  await expect(row(page, 'Review').getByRole('textbox', { name: 'Name of station 5' })).toBeFocused();
  await page.keyboard.press('Alt+ArrowUp');
  await page.keyboard.press('Alt+ArrowUp');
  expect(await order(page)).toEqual(['Inbox', 'Spec', 'Review', 'Approve spec', 'Build', 'Merge', 'Done']);

  // Nothing passes Done, by button or key.
  const mergeDown = row(page, 'Merge').getByRole('button', { name: 'Move Merge down' });
  await expect(mergeDown).toHaveAttribute('aria-disabled', 'true');
  await expect(mergeDown).toHaveAccessibleDescription('Nothing moves past Done');
  await row(page, 'Merge')
    .getByRole('textbox', { name: /^Name of station/ })
    .focus();
  await page.keyboard.press('Alt+ArrowDown');
  await expect(row(page, 'Done').getByRole('button', { name: 'Move Done up' })).toHaveAttribute(
    'aria-disabled',
    'true',
  );

  // 3. Drag the row number: Inbox goes under Spec.
  await drag(page, row(page, 'Inbox'), row(page, 'Spec'));
  expect(await order(page)).toEqual(['Spec', 'Inbox', 'Review', 'Approve spec', 'Build', 'Merge', 'Done']);

  // The keyboard drag: Space picks the row up, the arrows carry it, Space drops it.
  await row(page, 'Inbox')
    .getByRole('button', { name: /^Drag Inbox/ })
    .focus();
  const dndStatus = page.locator('[id^="DndLiveRegion"]');
  await page.keyboard.press('Space');
  // dnd-kit listens for the arrows a moment after the pick-up: press until the row is over Spec
  // (Spec is first, so a second press has nowhere further to go).
  await expect(async () => {
    await page.keyboard.press('ArrowUp');
    await expect(dndStatus).toContainText('Inbox is over Spec', { timeout: 200 });
  }).toPass();
  await page.keyboard.press('Space');
  await expect.poll(() => order(page)).toEqual(['Inbox', 'Spec', 'Review', 'Approve spec', 'Build', 'Merge', 'Done']);

  // The moves save on their own and survive a reload.
  await expect(status(page)).toContainText('Autosaved');
  await page.reload();
  await expect(rows(page).first()).toBeVisible();
  expect(await order(page)).toEqual(['Inbox', 'Spec', 'Review', 'Approve spec', 'Build', 'Merge', 'Done']);
});

test('the merge switch is disabled without a repository', async ({ page }) => {
  await openLine(page, '/o/acme/f/sre/definition/line');
  const approve = row(page, 'Approve');
  const merge = approve.getByRole('switch', { name: 'Merge step at Approve' });
  await expect(merge).toHaveAttribute('aria-disabled', 'true');
  await expect(merge).toHaveAccessibleDescription('Set a repository in Basics first');
  await merge.click({ force: true });
  await expect(merge).toHaveAttribute('aria-checked', 'false');
  await expect(approve.getByRole('link', { name: 'Basics' })).toBeVisible();
  await expectAccessible(page);

  // goal has a repository: Approve spec can become a merge step.
  await openLine(page);
  const spec = row(page, 'Approve spec').getByRole('switch', { name: 'Merge step at Approve spec' });
  await expect(spec).not.toHaveAttribute('aria-disabled', 'true');
  await spec.click();
  await expect(spec).toHaveAttribute('aria-checked', 'true');
  await expect(row(page, 'Approve spec')).toContainText('Approving merges the pull request');
});

test('add a station, name it, raise its WIP, add a check and change its kind', async ({ page }) => {
  await openLine(page);
  await row(page, 'Review')
    .getByRole('textbox', { name: /^Name of station/ })
    .focus();
  await page.getByRole('button', { name: 'Add station' }).click();
  await page.getByRole('menuitem', { name: 'Agent station' }).click();

  // It lands after the row that had focus, its name selected; the id follows the name while new.
  const added = row(page, 'New station');
  await expect(added).toHaveAccessibleName('6. New station, agent station');
  const name = added.getByRole('textbox', { name: 'Name of station 6' });
  await expect(name).toBeFocused();
  await page.keyboard.type('Security review');
  const fresh = row(page, 'Security review');
  await expect(fresh.getByText('id: security-review')).toBeVisible();
  await expect(fresh.getByRole('textbox', { name: 'Name of station 6' })).toBeFocused();

  await fresh.getByRole('button', { name: 'Raise the WIP limit of Security review' }).click();
  await expect(fresh.getByRole('textbox', { name: 'WIP limit of Security review' })).toHaveValue('2');

  await fresh.getByRole('button', { name: 'Add check to Security review' }).click();
  const dialog = page.getByRole('dialog', { name: 'Add a check to Security review' });
  await dialog.getByRole('textbox', { name: 'Command' }).fill('pnpm audit');
  await expectAccessible(page);
  await dialog.getByRole('button', { name: 'Add check' }).click();
  await expect(fresh.getByRole('button', { name: 'Remove check pnpm audit from Security review' })).toBeVisible();

  // A kind change keeps only what the new kind has, and says what went with Undo.
  await fresh.getByRole('radio', { name: 'Human' }).check();
  await expect(page.getByText('Security review is a human station now.')).toBeVisible();
  await expect(fresh.getByRole('button', { name: /Remove check/ })).toHaveCount(0);

  await expect(status(page)).toContainText('Autosaved');
  await expect(
    page.getByRole('navigation', { name: 'Definition sections' }).getByRole('link', { name: /^Line, \d+ changes/ }),
  ).toBeAttached();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});
