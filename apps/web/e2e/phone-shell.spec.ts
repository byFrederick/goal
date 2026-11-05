import type { Locator } from '@playwright/test';
import { expect, type Page, test } from '@playwright/test';
import { expectNoHorizontalScroll, orgNav } from './support';

// The phone shell (below 768 px): the Goal mark opens the drawer, the bottom tab bar follows the factory.
test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

test.describe('the phone shell at 375 px', () => {
  test.use({ viewport: { width: 375, height: 812 } });

  test('the Goal mark opens the drawer and returns focus to it on Escape', async ({ page }) => {
    await page.goto('/o/acme/f/goal');
    const mark = page.getByRole('button', { name: 'Open menu' });
    await mark.click();
    const drawer = page.getByRole('dialog', { name: 'Acme' });
    await expect(drawer).toBeVisible();
    await expect(drawer.getByRole('link', { name: 'Knowledge' })).toBeVisible();

    // Focus stays inside the drawer while it is open.
    await page.keyboard.press('Tab');
    await expect(drawer.locator(':focus')).toHaveCount(1);

    await page.keyboard.press('Escape');
    await expect(drawer).toBeHidden();
    await expect(mark).toBeFocused();
  });

  test('Needs you links to its page from the tab bar', async ({ page }) => {
    await page.goto('/o/acme/f/goal');
    const needs = page.getByRole('navigation', { name: 'Tabs' }).getByRole('link', { name: /^Needs you/ });
    await expect(needs).toHaveAttribute('href', '/o/acme/needs-you');
    await needs.click();
    await expect(page).toHaveURL(/\/o\/acme\/needs-you$/);
  });

  test('Floor, Board and Items follow the last factory visited outside a factory', async ({ page }) => {
    const tabs = page.getByRole('navigation', { name: 'Tabs' });
    await page.goto('/o/acme/f/sre/board');
    // The factory is remembered once its page has rendered; leave before that and nothing is remembered.
    await expect(tabs.getByRole('link', { name: 'Board' })).toHaveAttribute('href', '/o/acme/f/sre/board');
    await page.goto('/o/acme');
    await expect(tabs.getByRole('link', { name: 'Board' })).toHaveAttribute('href', '/o/acme/f/sre/board');
  });

  test('the page does not scroll sideways', async ({ page }) => {
    await page.goto('/o/acme/f/goal');
    await expectNoHorizontalScroll(page);
    await page.getByRole('button', { name: 'Open menu' }).click();
    await expect(page.getByRole('dialog', { name: 'Acme' })).toBeVisible();
    await expectNoHorizontalScroll(page);
  });

  test('the drawer lists the organization pages and the factories', async ({ page }) => {
    await page.goto('/o/acme');
    await page.getByRole('button', { name: 'Open menu' }).click();
    await expect(orgNav(page).filter({ visible: true }).getByRole('link', { name: 'Home' })).toBeVisible();
  });
});

test.describe('the phone shell at 390 px', () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test('the title stays visible next to the actions on Home and Runs, and the buttons are 44 px', async ({ page }) => {
    for (const [path, title] of [
      ['/o/acme', 'Home'],
      ['/o/acme/runs', 'Runs'],
    ] as const) {
      await page.goto(path);
      const bar = page.getByRole('banner');
      await expect(bar.getByText(title, { exact: true })).toBeVisible();
      for (const button of await bar.getByRole('button').all()) {
        const box = await button.boundingBox();
        expect(box?.height ?? 0).toBeGreaterThanOrEqual(43.5);
      }
    }
  });

  test('the top-bar action keeps a 16 px gutter on Home and Board', async ({ page }) => {
    for (const path of ['/o/acme', '/o/acme/f/goal/board']) {
      await page.goto(path);
      const bar = page.getByRole('banner');
      // The page's action arrives with its data; until then the bar holds the mark alone.
      await expect(bar.getByRole('button').nth(1)).toBeVisible();
      const mark = await bar.getByRole('button', { name: 'Open menu' }).boundingBox();
      expect(mark?.x ?? 0).toBeCloseTo(16, 0);
      const last = bar.getByRole('button').last();
      const box = await last.boundingBox();
      expect(390 - ((box?.x ?? 0) + (box?.width ?? 0))).toBeCloseTo(16, 0);
    }
  });

  test('Home shows New item as a compact 44 px button inside the screen, with no keycap', async ({ page }) => {
    await page.goto('/o/acme');
    const bar = page.getByRole('banner');
    const button = bar.getByRole('button', { name: 'New item' });
    await expect(button).toBeVisible();
    await expect(bar.locator('kbd')).toHaveCount(0);
    const box = await button.boundingBox();
    expect(box?.width ?? 0).toBeGreaterThanOrEqual(43.5);
    expect(box?.x ?? -1).toBeGreaterThanOrEqual(0);
    expect((box?.x ?? 0) + (box?.width ?? 0)).toBeLessThanOrEqual(390);
  });

  test('nothing scrolls sideways on a factory page', async ({ page }) => {
    await page.goto('/o/acme/f/goal/board');
    await expectNoHorizontalScroll(page);
  });
});

test.describe('the phone shell is gone at 768 px', () => {
  test.use({ viewport: { width: 768, height: 1024 } });

  test('no tab bar and no Goal mark button; the sidebar shows', async ({ page }) => {
    await page.goto('/o/acme/f/goal');
    await expect(page.getByRole('navigation', { name: 'Tabs' })).toBeHidden();
    await expect(page.getByRole('button', { name: 'Open menu' })).toBeHidden();
    await expect(orgNav(page).filter({ visible: true })).toBeVisible();
  });

  test('Home keeps the labelled New item button with its C keycap', async ({ page }) => {
    await page.goto('/o/acme');
    const button = page
      .getByRole('button', { name: /New item/ })
      .filter({ visible: true })
      .first();
    await expect(button).toContainText('New item');
    await expect(button.locator('kbd')).toHaveText('C');
  });
});

// The hit area of a control: its pseudo-element when `before` covers it (Button, Switch, touchTarget),
// else its own box (a padded link, an h-11 segment).
async function hitArea(control: Locator) {
  await control.scrollIntoViewIfNeeded();
  return control.evaluate((el) => {
    const box = el.getBoundingClientRect();
    const before = getComputedStyle(el, '::before');
    const covers = before.position === 'absolute' && before.content !== 'none';
    return {
      width: covers ? Math.max(box.width, Number.parseFloat(before.width)) : box.width,
      height: covers ? Math.max(box.height, Number.parseFloat(before.height)) : box.height,
    };
  });
}

test.describe('every phone control has a 44 px hit area at 390 px', () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  test('the pointer is coarse, so the Button hit area applies', async ({ page }) => {
    await page.goto('/o/acme');
    expect(await page.evaluate(() => matchMedia('(pointer: coarse)').matches)).toBe(true);
  });

  const samples: [string, string, (page: Page) => Locator][] = [
    ['Definition Form', '/o/acme/f/goal/definition/basics', (p) => p.getByRole('radio', { name: 'Form', exact: true })],
    ['Definition YAML', '/o/acme/f/goal/definition/basics', (p) => p.getByRole('radio', { name: 'YAML' })],
    ['Items Board', '/o/acme/f/goal/items', (p) => p.getByRole('link', { name: 'Board', exact: true }).first()],
    ['Items List', '/o/acme/f/goal/items', (p) => p.getByRole('button', { name: 'List', exact: true })],
    ['Runs filter', '/o/acme/runs', (p) => p.getByRole('button', { name: /^Failed/ }).first()],
    ['Access row title', '/o/acme/access', (p) => p.locator('a[href*="/definition/basics"]:visible').first()],
    [
      'Knowledge files filter',
      '/o/acme/knowledge/goal-docs',
      (p) => p.getByRole('searchbox', { name: 'Filter by path' }),
    ],
    ['Secret Show the value', '/o/acme/secrets?new=true', (p) => p.getByRole('button', { name: 'Show the value' })],
    ['Secrets New', '/o/acme/secrets', (p) => p.getByRole('banner').getByRole('button', { name: /New/ })],
    [
      'Secrets row',
      '/o/acme/secrets',
      (p) => p.locator('a[href^="/o/acme/secrets/"]:not([href$="/import"]):visible').first(),
    ],
    [
      'Secret Replace value',
      '/o/acme/secrets/github-token',
      (p) => p.getByRole('region', { name: 'Value' }).getByRole('button', { name: 'Replace value' }),
    ],
    ['Secret Delete', '/o/acme/secrets/github-token', (p) => p.getByRole('button', { name: 'Delete secret' })],
    [
      'New trigger source choice',
      '/o/acme/f/software/intake/new',
      (p) => p.getByRole('button', { name: 'Schedule', exact: true }),
    ],
    ['Knowledge source title', '/o/acme/knowledge', (p) => p.locator('a:visible', { hasText: 'goal-docs' }).first()],
    [
      'Knowledge new kind',
      '/o/acme/knowledge/new',
      (p) => p.locator('label', { has: p.getByRole('radio', { name: /Repository/ }) }),
    ],
    ['Home New item', '/o/acme', (p) => p.getByRole('banner').getByRole('button', { name: 'New item' })],
    ['Floor Pull andon', '/o/acme/f/goal', (p) => p.getByRole('banner').getByRole('button', { name: 'Pull andon' })],
    [
      'Agent Reset to default',
      '/o/acme/f/goal/agents/frontend-engineer',
      (p) => p.getByRole('button', { name: /^Reset to default, / }),
    ],
    [
      'Agent more skills',
      '/o/acme/f/goal/agents/frontend-engineer',
      (p) => p.getByRole('button', { name: /^\+ \d+ more$/ }),
    ],
    ['Definition line Send', '/o/acme/f/goal/definition/line', (p) => p.getByRole('button', { name: 'Send' })],
    ['Members switch', '/o/acme/members', (p) => p.getByRole('switch').first()],
    ['Floor Add a build agent', '/o/acme/f/goal', (p) => p.getByRole('button', { name: 'Add a build agent' })],
  ];

  for (const [name, path, find] of samples) {
    test(name, async ({ page }) => {
      await page.goto(path);
      const area = await hitArea(find(page));
      expect(area.height).toBeGreaterThanOrEqual(43.5);
      if (name.endsWith('switch') || name.includes('New item') || name.endsWith('Send'))
        expect(area.width).toBeGreaterThanOrEqual(43.5);
    });
  }
});

test.describe('phone fixes on the Assembly, Agent and Definition line pages at 390 px', () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  test('the assembly brief stays inside its box', async ({ page }) => {
    await page.goto('/o/acme/f/goal/items');
    await page.locator('a[href*="/items/"]').filter({ hasText: 'Usage and budgets' }).first().click();
    const brief = page.locator('p', { hasText: 'Show the week' }).first();
    const fit = await brief.evaluate((el) => ({
      scroll: el.scrollWidth,
      client: el.clientWidth,
      right: el.getBoundingClientRect().right,
      parentRight: el.parentElement?.getBoundingClientRect().right ?? 0,
    }));
    expect(fit.scroll).toBeLessThanOrEqual(fit.client);
    expect(fit.right).toBeLessThanOrEqual(fit.parentRight + 0.5);
  });

  test('a rejection reason carries its full text in a title', async ({ page }) => {
    await page.goto('/o/acme/f/goal/agents/frontend-engineer');
    const reasons = page.getByRole('region', { name: 'Why review sent work back' });
    // The reasons are the list's rows; the card's Ask Jonah button sits beside the heading, not in a row.
    const first = reasons.getByRole('listitem').first().getByRole('button');
    const title = await first.getAttribute('title');
    expect(title).toBeTruthy();
    const shown = (await first.textContent())?.trim() ?? '';
    expect(title?.toLowerCase().startsWith(shown.toLowerCase())).toBe(true);
  });

  test('the More actions icon is centred in its button', async ({ page }) => {
    await page.goto('/o/acme/f/goal/definition/line');
    const gaps = await page.getByRole('button', { name: 'More actions' }).evaluate((el) => {
      const box = el.getBoundingClientRect();
      const icon = (el.querySelector('svg') as SVGElement).getBoundingClientRect();
      return { left: icon.left - box.left, right: box.right - icon.right };
    });
    expect(Math.abs(gaps.left - gaps.right)).toBeLessThanOrEqual(1);
  });
});

test.describe('phone dialogs and rows at 390 px', () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  test('New trigger Close is 44 x 44', async ({ page }) => {
    await page.goto('/o/acme/f/software/intake/new');
    const area = await hitArea(page.getByRole('button', { name: 'Close', exact: true }));
    expect(area.width).toBeGreaterThanOrEqual(43.5);
    expect(area.height).toBeGreaterThanOrEqual(43.5);
  });

  test('a file row path ends before its size', async ({ page }) => {
    await page.goto('/o/acme/knowledge/goal-docs');
    const row = page
      .getByRole('table', { name: 'Files of goal-docs' })
      .getByRole('row')
      .filter({ has: page.getByRole('cell') })
      .first();
    const path = await row.getByRole('cell').nth(0).locator('span').first().boundingBox();
    const size = await row.getByRole('cell').nth(1).boundingBox();
    expect(path).not.toBeNull();
    expect(size).not.toBeNull();
    expect((path?.x ?? 0) + (path?.width ?? 0)).toBeLessThanOrEqual((size?.x ?? 0) + 0.5);
  });
});
