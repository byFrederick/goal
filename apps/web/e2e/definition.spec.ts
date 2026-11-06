// The Definition editor's frame and Basics (D6) against the mock: goal has a draft of v9 at
// revision 42 based on v8 (3 changes in 2 sections, 2 problems: one in Agents, one in Environments, ADR 0037).
import { expect, type Page, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll, isPhone, openNavigation, orgNav } from './support';
import { expectNoOverlap, expectTarget44 } from './support-boxes';

const basics = '/o/acme/f/goal/definition/basics';
const draft = '/v1/orgs/acme/factories/goal/draft';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

const status = (page: Page) => page.getByRole('status').filter({ hasText: /Based on|runs now/ });
const title = (page: Page) => page.getByRole('textbox', { name: 'Title' });
const tile = (page: Page, name: RegExp) =>
  page.getByRole('navigation', { name: 'Definition sections' }).getByRole('link', { name });

/** Floor, from the sidebar or, on a phone, the tab bar. */
const floorLink = (page: Page) =>
  page
    .getByRole('navigation', { name: isPhone(page) ? 'Tabs' : 'Organization and factories' })
    .getByRole('link', { name: 'Floor' });

async function openBasics(page: Page) {
  await page.goto(basics);
  await expect(page.getByRole('heading', { level: 1, name: 'Definition' })).toBeVisible();
  await expect(title(page)).toHaveValue('Goal');
}

test('the definition frame is accessible and fits the screen', async ({ page }) => {
  await openBasics(page);
  await expect(page).toHaveTitle('Basics · Definition · goal · Acme · Goal');
  await expect(page.getByRole('region', { name: 'Draft' })).toContainText('Draft of v9, 3 changes');
  await expect(status(page)).toContainText('Based on v8, the version running now.');
  await expect(page.getByRole('link', { name: '3 changes in 2 sections' })).toBeVisible();
  await expect(tile(page, /^Basics, goal, \d+ work types?$/)).toHaveAttribute('aria-current', 'location');
  await expect(tile(page, /^Agents, 1 change, 1 problem, \d+ agents? on \d+ models?$/)).toBeAttached();
  await expect(tile(page, /^Review and publish, Fix 2 problems first$/)).toBeAttached();
  await expect(page.getByRole('radio', { name: 'Form' })).toBeChecked();
  await expect(
    page.getByRole('textbox', { name: 'Name', description: 'Lowercase, used in links and the rail' }),
  ).toHaveValue('goal');
  await expect(page.getByText('6f0c2a7e-3d1b-4c55-9a7e-1f2b3c4d5e60')).toBeVisible();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);

  // Moving between sections focuses the section's heading.
  await tile(page, /^Line, 2 changes, \d+ stations?/).click();
  await expect(page).toHaveURL(/\/definition\/line$/);
  await expect(page.getByRole('heading', { level: 2, name: 'Line', exact: true })).toBeFocused();
  await expect(page.getByRole('list', { name: 'Stations, in order' })).toBeVisible();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

// The pictures for the pull request: attached to the report, not compared; the design exports are the reference.
for (const colorScheme of ['light', 'dark'] as const) {
  for (const size of [
    { width: 1440, height: 900 },
    { width: 390, height: 844 },
  ]) {
    test(`the definition frame at ${size.width} px, ${colorScheme} theme`, async ({ page }, testInfo) => {
      await page.emulateMedia({ colorScheme });
      await page.setViewportSize(size);
      await openBasics(page);
      const nav = page.getByRole('navigation', { name: 'Definition sections' });
      await expect(nav).toBeVisible();
      await expectNoHorizontalScroll(page);
      if (size.width < 640) {
        // One row of actions: Form | YAML, the primary action and More share a top offset.
        const view = await page.getByRole('group', { name: 'Editor view' }).boundingBox();
        const more = await page.getByRole('button', { name: 'More actions' }).boundingBox();
        expect(view && more && Math.abs(view.y + view.height / 2 - (more.y + more.height / 2)) < 4).toBe(true);
        expect(more?.height).toBeGreaterThanOrEqual(44);
        await expect(page.getByRole('button', { name: 'More actions' })).toHaveAttribute('aria-haspopup', 'menu');
      }
      await testInfo.attach(`definition-frame-${size.width}-${colorScheme}`, {
        body: await page.screenshot(),
        contentType: 'image/png',
      });
    });
  }
}

for (const colorScheme of ['light', 'dark'] as const) {
  test(`the Ask input and the status links are 44 px targets at 390 px, ${colorScheme} theme`, async ({ page }) => {
    await page.emulateMedia({ colorScheme });
    await page.setViewportSize({ width: 390, height: 844 });
    await openBasics(page);
    for (const target of [
      page.getByRole('textbox', { name: 'Ask the factory to change its definition' }),
      page.getByRole('link', { name: '3 changes in 2 sections' }),
      page.getByRole('link', { name: /^2 problems$/ }),
    ]) {
      await expect(target).toBeVisible();
      expect((await target.boundingBox())?.height).toBeGreaterThanOrEqual(44);
    }
    await expectAccessible(page);
    await expectNoHorizontalScroll(page);
  });
}

test('Definition opens the Line from the sidebar or the drawer', async ({ page }) => {
  // The h1 does not take focus after navigating from the phone drawer; the drawer's focus handling
  // (phone-chrome.tsx, route-focus.ts) belongs to the shell part.
  test.fixme(isPhone(page), 'Focus after drawer navigation is owned by software#693');
  await page.goto('/o/acme/f/goal');
  if (isPhone(page)) {
    // The drawer's factory group lists Definition under the open factory.
    await openNavigation(page);
    await orgNav(page).getByRole('link', { name: 'Definition' }).click();
  } else {
    await orgNav(page).getByRole('link', { name: 'Definition' }).click();
  }
  await expect(page).toHaveURL(/\/o\/acme\/f\/goal\/definition\/line$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Definition' })).toBeFocused();
});

test('Basics: changes save on their own, count on the tile and survive a reload', async ({ page }) => {
  await openBasics(page);

  await title(page).fill('Goal platform');
  const addType = page.getByRole('textbox', { name: 'Add a work type' });
  await addType.fill('Docs');
  await addType.press('Enter');
  await expect(page.getByRole('alert')).toHaveText(/lowercase letters, digits and dashes/);
  await addType.fill('docs');
  await addType.press('Enter');
  await page.getByRole('button', { name: 'Remove work type design' }).click();

  await expect(status(page)).toContainText('Autosaved');
  await expect(tile(page, /^Basics, [2-9] changes, /)).toBeAttached();
  await expect(page.getByRole('link', { name: /^\d+ changes in 3 sections$/ })).toBeVisible();

  await page.reload();
  await expect(title(page)).toHaveValue('Goal platform');
  const types = page.getByRole('group', { name: 'Work types' });
  await expect(types.getByRole('button', { name: 'Remove work type docs' })).toBeVisible();
  await expect(types.getByRole('button', { name: 'Remove work type design' })).toHaveCount(0);

  // Edited · Undo puts the running version's title back.
  await page.getByRole('button', { name: 'Undo the change to the title' }).click();
  await expect(title(page)).toHaveValue('Goal');
});

test('leaving with a change goald has not saved asks first, and Retry saves it', async ({ page, request }) => {
  await request.post('/__mock/fail', {
    data: { path: '/draft/model/basics', method: 'PUT', status: 503, times: 0 },
  });
  await openBasics(page);

  await title(page).fill('Goal 2');
  await expect(status(page)).toContainText('Not saved: the save failed.');
  await floorLink(page).click();

  const dialog = page.getByRole('dialog', { name: 'Your last changes aren’t saved yet' });
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText('The save failed.');
  await expectAccessible(page);

  await request.delete('/__mock/fail');
  await dialog.getByRole('button', { name: 'Retry' }).click();
  await expect(page).toHaveURL(/\/o\/acme\/f\/goal$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Floor' })).toBeVisible();

  await page.goto(basics);
  await expect(title(page)).toHaveValue('Goal 2');
});

test('a field changed here and elsewhere asks which value to keep', async ({ page, request }) => {
  await openBasics(page);

  await title(page).fill('Mine');
  // Another tab saves the same field before this one's autosave goes out.
  const model = await (await request.get(`${draft}/model`)).json();
  const saved = await request.put(`${draft}/model/basics`, {
    data: { value: { ...model.model.basics, title: 'Theirs' } },
    headers: { 'If-Match': `"${model.etags.basics}"` },
  });
  expect(saved.ok()).toBe(true);

  const dialog = page.getByRole('dialog', { name: 'Changed here and elsewhere' });
  await expect(dialog).toBeVisible();
  const row = dialog.getByRole('group', { name: /^Title also changed elsewhere: theirs Theirs, yours Mine$/ });
  await expect(row).toBeVisible();
  await expectAccessible(page);
  await row.getByRole('radio', { name: 'Keep theirs' }).check();
  await dialog.getByRole('button', { name: 'Save these choices' }).click();

  await expect(dialog).toBeHidden();
  await expect(title(page)).toHaveValue('Theirs');
  await expect(status(page)).not.toContainText('Not saved');
});

test('discard the draft: the running version shows, and the next change opens a new draft', async ({ page }) => {
  await openBasics(page);
  const review = page.getByRole('link', { name: 'Review changes', exact: true }).first();
  await expect(review).not.toHaveAccessibleDescription('No changes yet');

  await page.getByRole('button', { name: 'Discard draft', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Discard the draft of v9?' });
  await expect(dialog).toContainText('3 changes in 2 sections are lost. v8 keeps running.');
  await dialog.getByRole('button', { name: 'Discard draft' }).click();

  await expect(dialog).toBeHidden();
  await expect(status(page)).toContainText('v8 runs now. Your first change opens a draft.');
  await expect(page.getByText('v8 running', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Discard draft', exact: true })).toHaveCount(0);
  await expect(tile(page, /^Line, \d+ stations?/)).toBeAttached();
  // Nothing to publish: Review changes is not the page's primary action.
  await expect(review).toHaveAccessibleDescription('No changes yet');

  await title(page).fill('Fresh start');
  await expect(status(page)).toContainText('Based on v8, the version running now.');
  await expect(status(page)).toContainText('Autosaved');
  await expect(page.getByRole('region', { name: 'Draft' })).toContainText('Draft of v9');
  await expect(tile(page, /^Basics, 1 change, /)).toBeAttached();
  await expect(review).not.toHaveAccessibleDescription('No changes yet');
});

test('a draft discarded elsewhere keeps the changes here for a new draft', async ({ page, request }) => {
  await openBasics(page);

  await request.delete(draft, { headers: { 'If-Match': '"42"' } });
  const dialog = page.getByRole('dialog', { name: 'This draft was discarded in another tab or the CLI' });
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: 'Start a new draft' }).click();

  await title(page).fill('After the discard');
  await expect(status(page)).toContainText('Autosaved');
  await expect(tile(page, /^Basics, 1 change, /)).toBeAttached();
});

test('the YAML editor at 375 px: the file tree waits behind Files, and the Outline is left out', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 800 });
  await page.goto('/o/acme/f/goal/definition/line?mode=yaml');
  const files = page.getByRole('button', { name: 'Files', exact: true });
  await expect(files).toHaveAttribute('aria-expanded', 'false');
  await expect(page.getByRole('navigation', { name: 'Definition files' })).toBeHidden();
  await expect(page.getByRole('complementary', { name: 'Outline' })).toBeHidden();
  await expectNoHorizontalScroll(page);

  await files.click();
  await expect(page.getByRole('navigation', { name: 'Definition files' })).toBeVisible();
  await expectAccessible(page);
});

test('Ask the factory is not built yet: the input is disabled and Ask stays unavailable', async ({ page }) => {
  await page.goto(basics);
  const input = page.getByRole('textbox', { name: 'Ask the factory to change its definition' });
  await expect(input).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Send', exact: true })).toHaveAttribute('aria-disabled', 'true');
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

// The Agents and Versions sections at 1440 and 390 px, in both themes (the pictures are the
// review's evidence; the assertions are what the audit of 2026-11-02 found broken).
for (const width of [1440, 390]) {
  for (const scheme of ['light', 'dark'] as const) {
    test(`Agents and Versions at ${width}px in ${scheme}`, async ({ page }) => {
      await page.setViewportSize({ width, height: width === 1440 ? 1000 : 844 });
      await page.emulateMedia({ colorScheme: scheme });

      await page.goto('/o/acme/f/goal/definition/agents');
      const list = page.getByRole('navigation', { name: 'Agents' });
      await expect(list).toBeVisible();
      const name = list.getByRole('link').first().locator('[translate="no"]');
      await expect(name).toBeVisible();
      expect(await name.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true);
      await expectNoHorizontalScroll(page);
      await page.screenshot({ path: `test-results/definition-agents-${width}-${scheme}.png`, fullPage: true });

      await page.goto('/o/acme/f/goal/definition/versions');
      await expect(page.getByText('v8', { exact: true }).first()).toBeVisible();
      await expectNoHorizontalScroll(page);
      await page.screenshot({ path: `test-results/definition-versions-${width}-${scheme}.png`, fullPage: true });
    });
  }
}

test('Review and publish with no draft shows Checks and Versions, and no Budget', async ({ page, request }) => {
  await request.delete(draft, { headers: { 'If-Match': '"42"' } });
  for (const [width, height] of [
    [1440, 900],
    [390, 844],
  ] as const) {
    await page.setViewportSize({ width, height });
    for (const scheme of ['light', 'dark'] as const) {
      await page.emulateMedia({ colorScheme: scheme });
      await page.goto('/o/acme/f/goal/definition/review');
      await expect(page.getByRole('heading', { level: 2, name: 'Review and publish' })).toBeVisible();
      await expect(page.getByRole('heading', { name: 'Checks' })).toBeVisible();
      await expect(page.getByRole('heading', { name: 'Versions' })).toBeVisible();
      await expect(page.getByRole('link', { name: /^All (\d+ )?versions$/ })).toBeVisible();
      await expect(page.getByRole('heading', { name: 'Budget' })).toHaveCount(0);
      await expectNoHorizontalScroll(page);
      await page.screenshot({ path: `test-results/definition-review-${width}-${scheme}.png`, fullPage: true });
    }
  }
});

// The pictures for the pull request: the Line at desktop and phone width, in both themes. They are
// attached to the report, not compared; docs/design/screens/build/definition-form.jpg is the reference.
for (const theme of ['light', 'dark'] as const) {
  for (const [name, size] of [
    ['1440', { width: 1440, height: 900 }],
    ['390', { width: 390, height: 844 }],
  ] as const) {
    test(`line screenshot at ${name}, ${theme}`, async ({ page }, testInfo) => {
      await page.setViewportSize(size);
      await page.addInitScript((value) => window.localStorage.setItem('goal-theme', value), theme);
      await page.goto('/o/acme/f/goal/definition/line');
      await expect(page.getByRole('list', { name: 'Stations, in order' })).toBeVisible();
      await expect(page.getByRole('region', { name: 'Checks on this line' })).toBeVisible();
      // The table scrolls in its own box; the page does not scroll sideways.
      const wide = await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth);
      expect(wide).toBe(true);
      await testInfo.attach(`line-${name}-${theme}`, {
        body: await page.screenshot({ fullPage: true }),
        contentType: 'image/png',
      });
    });
  }
}

// The line section at every width: the page never scrolls sideways, the current tile stays inside the
// stepper's box, and on a phone each row's controls do not overlap.
for (const colorScheme of ['light', 'dark'] as const) {
  for (const width of [1440, 1280, 1024, 768, 390]) {
    test(`the line section at ${width} px, ${colorScheme}: no sideways scroll, no overlap`, async ({ page }) => {
      await page.emulateMedia({ colorScheme });
      await page.setViewportSize({ width, height: 900 });
      await page.goto('/o/acme/f/goal/definition/line');
      await expect(page.getByRole('list', { name: 'Stations, in order' })).toBeVisible();
      await expectNoHorizontalScroll(page);

      const nav = page.getByRole('navigation', { name: 'Definition sections' });
      const navBox = await nav.boundingBox();
      const current = await nav.locator('[aria-current]').boundingBox();
      expect(navBox && current).toBeTruthy();
      if (navBox && current) {
        expect(current.x).toBeGreaterThanOrEqual(navBox.x - 1);
        expect(current.x + current.width).toBeLessThanOrEqual(navBox.x + navBox.width + 1);
      }

      if (width < 640) {
        const rows = page.getByRole('list', { name: 'Stations, in order' }).getByRole('listitem');
        const count = await rows.count();
        for (let i = 0; i < count; i++) {
          const row = rows.nth(i);
          const whoLocator = row.getByRole('combobox', { name: /^Who works at / });
          const upLocator = row.getByRole('button', { name: /^Move .* up$/ });
          const who = (await whoLocator.count()) > 0 ? await whoLocator.first().boundingBox() : null;
          const up = (await upLocator.count()) > 0 ? await upLocator.first().boundingBox() : null;
          const more = row.getByRole('button', { name: /^More for / });
          const moreBox = await more.boundingBox();
          if (who && up) {
            const apart =
              who.x + who.width <= up.x ||
              up.x + up.width <= who.x ||
              who.y + who.height <= up.y ||
              up.y + up.height <= who.y;
            expect(apart).toBe(true);
          }
          if (moreBox) {
            expect(moreBox.width).toBeGreaterThanOrEqual(44);
            expect(moreBox.height).toBeGreaterThanOrEqual(44);
            const icon = await more.locator('svg').boundingBox();
            if (icon) {
              const left = icon.x - moreBox.x;
              const right = moreBox.x + moreBox.width - (icon.x + icon.width);
              expect(Math.abs(left - right)).toBeLessThanOrEqual(1);
            }
          }
        }
      }
    });
  }
}

for (const theme of ['light', 'dark'] as const) {
  for (const [name, size] of [
    ['1440', { width: 1440, height: 900 }],
    ['390', { width: 390, height: 844 }],
  ] as const) {
    test(`the YAML view at ${name} px in ${theme}: Problems, a one-row ask bar, no overflow`, async ({
      page,
    }, testInfo) => {
      await page.emulateMedia({ colorScheme: theme });
      await page.setViewportSize(size);
      await page.goto('/o/acme/f/goal/definition/line?mode=yaml');
      await expect(page.getByRole('region', { name: /Problems/ })).toBeVisible();
      const bar = await page
        .getByRole('textbox', { name: 'Ask the factory to change its definition' })
        .locator('xpath=..')
        .boundingBox();
      expect(bar?.height ?? 0).toBeLessThan(56);
      await expectAccessible(page);
      await expectNoHorizontalScroll(page);
      await testInfo.attach(`yaml-${name}-${theme}`, {
        body: await page.screenshot({ fullPage: true }),
        contentType: 'image/png',
      });
    });
  }
}

test('the change-count and problems links are 44×44 at 390 px', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await openBasics(page);
  // The links sit beside the live region, not in it (the region would announce them).
  const bar = status(page).locator('xpath=..');
  await expectTarget44(bar.getByRole('link', { name: /\d+ changes? in \d+ sections?/ }), 'the change count');
  await expectTarget44(bar.getByRole('link', { name: /\d+ problems?/ }), 'the problems link');
  await expectNoOverlap(
    bar.getByRole('link', { name: /\d+ changes? in \d+ sections?/ }),
    bar.getByRole('link', { name: /\d+ problems?/ }),
    'the change count and the problems link',
  );
});
