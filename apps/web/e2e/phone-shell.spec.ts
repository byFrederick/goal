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
    await page.goto('/o/acme/f/sre/board');
    await page.goto('/o/acme');
    const tabs = page.getByRole('navigation', { name: 'Tabs' });
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
    [
      'Definition Form',
      '/o/acme/f/goal/definition/basics',
      (p) => p.getByRole('button', { name: 'Form', exact: true }),
    ],
    ['Definition YAML', '/o/acme/f/goal/definition/basics', (p) => p.getByRole('button', { name: 'YAML' })],
    ['Items Board', '/o/acme/f/goal/items', (p) => p.getByRole('link', { name: 'Board', exact: true }).first()],
    ['Items List', '/o/acme/f/goal/items', (p) => p.getByRole('button', { name: 'List', exact: true })],
    ['Runs filter', '/o/acme/runs', (p) => p.getByRole('button', { name: /^Failed/ }).first()],
    [
      'Connections row title',
      '/o/acme/connections',
      (p) => p.locator('a[href^="/o/acme/connections/"]:not([href$="/new"]):visible').first(),
    ],
    ['Knowledge document title', '/o/acme/knowledge/goal-docs', (p) => p.locator('a[href*="document="]').first()],
    ['Knowledge source title', '/o/acme/knowledge', (p) => p.locator('a:visible', { hasText: 'goal docs' }).first()],
    [
      'Knowledge new kind',
      '/o/acme/knowledge/new',
      (p) => p.locator('label', { has: p.getByRole('radio', { name: /Synced/ }) }),
    ],
    ['Home New item', '/o/acme', (p) => p.getByRole('banner').getByRole('button', { name: 'New item' })],
    [
      'Floor Pull andon',
      '/o/acme/f/goal',
      (p) => p.getByRole('banner').getByRole('button', { name: 'Pull the andon cord' }),
    ],
    ['Members switch', '/o/acme/members', (p) => p.getByRole('switch').first()],
    ['Floor Add a build agent', '/o/acme/f/goal', (p) => p.getByRole('button', { name: 'Add a build agent' })],
  ];

  for (const [name, path, find] of samples) {
    test(name, async ({ page }) => {
      await page.goto(path);
      const area = await hitArea(find(page));
      expect(area.height).toBeGreaterThanOrEqual(43.5);
      if (name.endsWith('switch') || name.includes('New item')) expect(area.width).toBeGreaterThanOrEqual(43.5);
    });
  }
});
