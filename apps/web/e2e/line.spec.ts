// The Line section (D7) against the mock: goal's draft of v9 has Inbox, Spec, Approve spec,
// Build, Review, Merge (a merge step) and Done; sre has no repository and no draft.
import { expect, type Locator, type Page, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll, pressUntilHeard } from './support';

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
  await openRules(page);
}

/** Opens every station's rules, which start collapsed under its row. */
async function openRules(page: Page) {
  for (const toggle of await stations(page)
    .getByRole('button', { name: /^Rules of / })
    .all()) {
    if ((await toggle.getAttribute('aria-expanded')) === 'false') await toggle.click();
  }
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
  await expect(row(page, 'Build').getByRole('combobox', { name: 'Who works here at Build' })).toContainText(
    'routed by lane',
  );
  await expect(row(page, 'Review').getByRole('combobox', { name: 'If rejected, send back to' })).toHaveText('Build');
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
  await pressUntilHeard(
    page,
    'ArrowUp',
    dndStatus.filter({ hasText: 'Inbox is over Inbox' }),
    dndStatus.filter({ hasText: 'Inbox is over Spec' }),
  );
  await page.keyboard.press('Space');
  await expect.poll(() => order(page)).toEqual(['Inbox', 'Spec', 'Review', 'Approve spec', 'Build', 'Merge', 'Done']);

  // The moves save on their own and survive a reload.
  await expect(status(page)).toContainText('Autosaved');
  await page.reload();
  await expect(rows(page).first()).toBeVisible();
  expect(await order(page)).toEqual(['Inbox', 'Spec', 'Review', 'Approve spec', 'Build', 'Merge', 'Done']);
});

test('a factory without a draft opens its definition without a failed request', async ({ page }) => {
  const errors: string[] = [];
  page.on('console', (m) => {
    if (m.type() === 'error') errors.push(m.text());
  });
  page.on('response', (r) => {
    if (r.status() >= 400) errors.push(`${r.status()} ${new URL(r.url()).pathname}`);
  });

  // sre runs v3 with no draft open: "no draft" is a state, not an error to fetch and fail.
  await openLine(page, '/o/acme/f/sre/definition/line');
  await expect(status(page)).toContainText('runs now');
  await page.goto('/o/acme/f/sre/definition/review');
  await expect(page.getByRole('region', { name: 'Publish' })).toContainText('No draft. v3 is running.');
  expect(errors).toEqual([]);
});

test('the merge switch is disabled without a repository', async ({ page }) => {
  await openLine(page, '/o/acme/f/sre/definition/line');
  const approve = row(page, 'Approve');
  const merge = approve.getByRole('switch', { name: 'Merge step at Approve' });
  await expect(merge).toHaveAttribute('aria-disabled', 'true');
  await expect(merge).toHaveAccessibleDescription('Needs a repository with write access, set in Basics');
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
  await expect(row(page, 'Approve spec')).toContainText('A person merges the pull request');
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

  await fresh.getByRole('button', { name: 'Rules of Security review' }).click();
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

for (const width of [1024, 1280, 1440]) {
  test(`fits the stations table in its card at ${width} px without clipped columns`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await openLine(page);
    const box = stations(page).locator('xpath=ancestor::div[contains(@class,"overflow-x-auto")][1]');
    const fits = await box.evaluate((el) => el.scrollWidth <= el.clientWidth);
    expect(fits, 'the stations box does not scroll sideways').toBe(true);
    const first = rows(page).first();
    await first.getByRole('textbox', { name: /^Name of station/ }).fill('Integrate');
    const report = await box.evaluate((el) => {
      const rect = (e: Element) => e.getBoundingClientRect();
      const overlap = (a: DOMRect, b: DOMRect) =>
        a.left < b.right - 1 && b.left < a.right - 1 && a.top < b.bottom - 1 && b.top < a.bottom - 1;
      const visible = (e: Element) => rect(e).width > 0 && rect(e).height > 0;
      const right = rect(el).right;
      const rows = Array.from(el.querySelectorAll('ol > li'));
      const names = rows.map((r) => r.querySelector<HTMLInputElement>('[data-station-name]')).filter((n) => n !== null);
      const headers = Array.from(el.querySelectorAll('[aria-hidden="true"].bg-panel > span')).filter(visible);
      const headerOverlap = headers.some((h, i) => headers.slice(i + 1).some((o) => overlap(rect(h), rect(o))));
      // Cells of the first row that sit on the same line must not overlap.
      const cells = Array.from(rows[0]?.querySelector('div')?.children ?? []).flatMap((c) =>
        getComputedStyle(c).display === 'contents' ? Array.from(c.children) : [c],
      );
      const sameLineOverlap = cells.some((c, i) =>
        cells.slice(i + 1).some((o) => Math.abs(rect(c).top - rect(o).top) < 8 && overlap(rect(c), rect(o))),
      );
      const width = (selector: string) =>
        Math.min(...rows.flatMap((r) => Array.from(r.querySelectorAll(selector)).map((f) => rect(f).width)));
      return {
        scrolls: el.scrollWidth > el.clientWidth,
        clipped: names.filter((n) => n.scrollWidth > n.clientWidth).length,
        nameWidth: width('[data-station-name]'),
        kindWidth: width('fieldset[class*="w-full"]'),
        whoWidth: width('[aria-label^="Who works here at"]'),
        headerOverlap,
        sameLineOverlap,
        inside: cells.every((c) => rect(c).right <= right + 1),
      };
    });
    expect(report.scrolls, 'the stations box does not scroll sideways').toBe(false);
    expect(report.clipped, 'no name input clips "Integrate"').toBe(0);
    expect(report.headerOverlap, 'no two column headers overlap').toBe(false);
    expect(report.sameLineOverlap, 'cells on one line do not overlap').toBe(false);
    expect(report.inside, 'every cell of the first row ends inside the card').toBe(true);
    expect(report.nameWidth, 'the name field keeps its minimum').toBeGreaterThanOrEqual(150);
    expect(report.kindWidth, 'the kind field keeps its minimum').toBeGreaterThanOrEqual(220);
    expect(report.whoWidth, 'the who-works field keeps its minimum').toBeGreaterThanOrEqual(200);
  });
}
