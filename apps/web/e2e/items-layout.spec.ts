// Items against the mock: Title gets the leftover width at 1024 to 1440, the view tabs stay on one line at 768, and the
// pictures for the pull request.
import { expect, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll, lengthenTitles } from './support';
import { cutRefs, expectNoOverlap, expectTarget44, shownTitleCharacters } from './support-boxes';

const items = '/o/acme/f/goal/items';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

test.describe('desktop', () => {
  test('shows State and Cost inside the table box at 1440', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'desktop-1440', 'the 1440 project');
    await page.goto(items);
    const table = page.getByRole('table', { name: 'Items' });
    await expect(table).toBeVisible();
    const box = await table.locator('xpath=..').boundingBox();
    for (const name of ['State', 'Cost']) {
      const header = table.getByRole('columnheader', { name, exact: true });
      await expect(header).toBeVisible();
      const at = await header.boundingBox();
      expect(at && box && at.x + at.width <= box.x + box.width + 1).toBe(true);
    }
    const scrolls = await table.locator('xpath=..').evaluate((el) => el.scrollWidth - el.clientWidth);
    expect(scrolls).toBeLessThanOrEqual(0);
  });
});

for (const { width, height } of [
  { width: 1024, height: 800 },
  { width: 1280, height: 900 },
  { width: 1440, height: 1000 },
]) {
  test(`gives Title the leftover width without sideways scroll at ${width} px with long titles`, async ({ page }) => {
    await page.setViewportSize({ width, height });
    await lengthenTitles(page, /\/v1\/orgs\/[^/]+\/factories\/[^/]+\/items(\?|$)/);
    await page.goto(items);
    const table = page.getByRole('table', { name: 'Items' });
    // Type, Lane, Agent and Age leave the columns when the table's box is narrow (below).
    for (const name of ['Title', 'Station', 'State', 'Cost']) {
      await expect(table.getByRole('columnheader', { name, exact: true })).toBeVisible();
    }
    // The title link stays inside its cell: its box does not run under the Type or Lane cell.
    const row = table
      .locator('tbody tr')
      .filter({ has: page.getByRole('link') })
      .first();
    // Cells are found by their header's index: a Repositories cell may sit after Title.
    const headers = await table.locator('thead th').allInnerTexts();
    const at = (name: string) => headers.findIndex((h) => h.trim().startsWith(name));
    const link = row.locator('td').nth(at('Title')).getByRole('link').first();
    if (await table.getByRole('columnheader', { name: 'Type', exact: true }).isVisible()) {
      await expectNoOverlap(link, row.locator('td').nth(at('Type')), 'the title link and the Type cell');
      await expectNoOverlap(link, row.locator('td').nth(at('Lane')), 'the title link and the Lane cell');
    }
    // Title takes the leftover width: 30 characters of every title show, no ref is cut, and no
    // column is narrower than its minimum (a narrow table puts Type, Lane, Agent and Age under the title).
    const titles = await shownTitleCharacters(table, 'tbody td a.truncate, tbody td a span.truncate');
    expect(titles.length).toBeGreaterThan(0);
    for (const t of titles) expect(t.shown, `"${t.text}" shows ${t.shown} characters`).toBeGreaterThanOrEqual(t.needed);
    expect(await cutRefs(table)).toEqual([]);
    const minimum: Record<string, number> = {
      Ref: 90,
      Title: 240,
      Type: 70,
      Lane: 80,
      Station: 100,
      State: 130,
      Agent: 100,
      Age: 56,
      Cost: 70,
    };
    for (const [name, least] of Object.entries(minimum)) {
      const header = table.getByRole('columnheader', { name, exact: true });
      if (!(await header.isVisible())) continue;
      const at = await header.boundingBox();
      expect(at?.width ?? 0, `the ${name} column`).toBeGreaterThanOrEqual(least);
    }
    // A hidden column's values show on a second line under the title, so no value is lost.
    if (!(await table.getByRole('columnheader', { name: 'Lane', exact: true }).isVisible())) {
      await expect(row.getByText('Lane:')).toBeAttached();
    }
    const fits = await table.evaluate((t) => {
      const box = t.parentElement as HTMLElement;
      return t.scrollWidth <= box.clientWidth && box.scrollWidth <= box.clientWidth;
    });
    expect(fits).toBe(true);
  });
}

test('keeps the view tabs on one line at 768', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'tablet-768', 'the 768 project');
  await page.goto(items);
  const nav = page.getByRole('navigation', { name: 'Saved views' });
  await expect(nav).toBeVisible();
  const tabs = nav.getByRole('link').or(nav.getByRole('button', { name: '+ Save view' }));
  const boxes = await tabs.evaluateAll((els) => els.map((el) => el.getBoundingClientRect()));
  expect(boxes.length).toBeGreaterThan(3);
  expect(new Set(boxes.map((b) => Math.round(b.top))).size).toBe(1);
  expect(new Set(boxes.map((b) => Math.round(b.height))).size).toBe(1);
  await expectNoHorizontalScroll(page);
});

// Attached to the report, not compared; the design exports are the reference.
for (const theme of ['light', 'dark'] as const) {
  test.describe(`screenshots, ${theme}`, () => {
    test(`items at the project's width, ${theme}`, async ({ page }, testInfo) => {
      test.skip(testInfo.project.name !== 'desktop-1440', 'the 1440 project');
      await page.addInitScript((value) => window.localStorage.setItem('goal-theme', value), theme);
      await page.goto(items);
      await expect(page.getByRole('heading', { level: 1, name: 'Items' })).toBeVisible();
      await testInfo.attach(`items-1440-${theme}`, {
        body: await page.screenshot({ fullPage: true }),
        contentType: 'image/png',
      });
    });

    test.describe('phone', () => {
      test.use({ viewport: { width: 390, height: 844 } });
      test(`items at 390, ${theme}`, async ({ page }, testInfo) => {
        test.skip(testInfo.project.name !== 'phone-375', 'the phone project');
        await page.addInitScript((value) => window.localStorage.setItem('goal-theme', value), theme);
        await page.goto(items);
        await expect(page.getByRole('heading', { level: 1, name: 'Items' })).toBeVisible();
        await testInfo.attach(`items-390-${theme}`, {
          body: await page.screenshot({ fullPage: true }),
          contentType: 'image/png',
        });
      });
    });
  });
}

test('keeps every item title inside its cell with long titles at 768 and 390', async ({ page }, testInfo) => {
  test.skip(!['tablet-768', 'phone-375'].includes(testInfo.project.name), 'the 768 and phone projects');
  await lengthenTitles(page, /\/v1\/orgs\/[^/]+\/factories\/[^/]+\/items(\?|$)/);
  await page.goto(items);
  const table = page.getByRole('table', { name: 'Items' });
  await expect(table).toBeVisible();
  const spills = await table.evaluate((t) =>
    Array.from(t.querySelectorAll<HTMLElement>('tbody td [title]'))
      .filter((el) => el.closest('td')?.querySelector('a') && el.tagName !== 'TD')
      .map((el) => ({ el, cell: el.closest('td') as HTMLElement }))
      .filter(({ el, cell }) => el.scrollWidth > cell.clientWidth)
      .map(({ el }) => el.textContent?.slice(0, 40)),
  );
  expect(spills).toEqual([]);
});

for (const colorScheme of ['light', 'dark'] as const) {
  test(`keeps each assembly row's content inside its row at 390 px, ${colorScheme}`, async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.emulateMedia({ colorScheme });
    await lengthenTitles(page, /\/v1\/orgs\/[^/]+\/factories\/[^/]+\/items(\?|$)/);
    await page.goto(items);
    const links = page.getByRole('table', { name: 'Items' }).getByRole('link', { name: /^Assembly,/ });
    await expect(links.first()).toBeVisible();
    const count = await links.count();
    for (let i = 0; i < count; i++) {
      const link = links.nth(i);
      const link$ = await link.boundingBox();
      const row$ = await link.locator('xpath=ancestor::tr[1]').boundingBox();
      expect(link$ && row$ && link$.y >= row$.y - 0.5 && link$.y + link$.height <= row$.y + row$.height + 0.5).toBe(
        true,
      );
      expect(link$?.height ?? 0).toBeGreaterThanOrEqual(43.5);
    }
  });
}

for (const colorScheme of ['light', 'dark'] as const) {
  test(`gives row checkboxes 44 px targets that do not overlap at 390 px, ${colorScheme}`, async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.emulateMedia({ colorScheme });
    await page.goto(items);
    const boxes = page.getByRole('table', { name: 'Items' }).getByRole('checkbox', { name: /^Select / });
    await expect(boxes.first()).toBeVisible();
    const first = boxes.nth(0).locator('xpath=..');
    const second = boxes.nth(1).locator('xpath=..');
    await expectTarget44(first, 'the first row checkbox');
    await expectNoOverlap(first, second, 'adjacent row checkboxes');
    await expectAccessible(page);
  });
}

for (const { width, height } of [
  { width: 390, height: 844 },
  { width: 1440, height: 1000 },
]) {
  test(`keeps an Items row as tall as with a plain span around the checkbox at ${width} px`, async ({ page }) => {
    await page.setViewportSize({ width, height });
    await page.goto(items);
    const box = page
      .getByRole('table', { name: 'Items' })
      .getByRole('checkbox', { name: /^Select / })
      .first();
    await expect(box).toBeVisible();
    const row = box.locator('xpath=ancestor::tr[1]');
    const before = (await row.boundingBox())?.height ?? 0;
    await box.locator('xpath=..').evaluate((label) => {
      const span = document.createElement('span');
      span.className = label.className;
      span.append(...Array.from(label.childNodes));
      label.replaceWith(span);
    });
    const after = (await row.boundingBox())?.height ?? 0;
    expect(before).toBeGreaterThan(0);
    expect(Math.abs(before - after)).toBeLessThanOrEqual(0.5);
  });
}
