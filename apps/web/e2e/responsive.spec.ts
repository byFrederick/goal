// No page scrolls sideways (MASTER.md "Layout" and "Accessibility and checks"): the document is never wider than
// the window (documentElement.scrollWidth against clientWidth, which counts a button's hit area), at 1440, 1280, 1024, 768 and 390 px wide, in light and
// dark, against the mock. The routes: Runs, Items, Needs you, Item (/items/12), New trigger, Jonah, Home,
// Definition line, Board, Account, Factory settings, Assembly item (/items/17), Agents, Agent (reviewer),
// Connections, GitHub connection, Knowledge source (handbook) and Spend. A wide table scrolls in its own box, so
// the document's width is asserted, and so is that no visible element ends past the right edge outside a
// horizontal scroller (expectNoClippedElements): the shell clips overflow, so scrollWidth alone misses cut-off content. Each visit attaches a screenshot. Not run in the box (no browser): the
// pull request says so.
import { expect, type Page, test } from '@playwright/test';
import { expectNoClippedElements, expectNoHorizontalScroll } from './support';

const widths = [
  { width: 1440, height: 1000 },
  { width: 1280, height: 800 },
  { width: 1024, height: 768 },
  { width: 768, height: 1024 },
  { width: 390, height: 844 },
];

const pages: { name: string; open: (page: Page) => Promise<void> }[] = [
  {
    name: 'Runs',
    open: async (page) => {
      await page.goto('/o/acme/runs');
      await expect(page.getByRole('heading', { level: 1, name: 'Runs' })).toBeVisible();
      await expect(page.getByRole('table')).toBeVisible();
      await expect(page.getByRole('button', { name: 'Export CSV' }).filter({ visible: true })).toBeVisible();
    },
  },
  {
    name: 'Items',
    open: async (page) => {
      await page.goto('/o/acme/f/goal/items');
      await expect(page.getByRole('heading', { level: 1, name: /items/i })).toBeVisible();
    },
  },
  {
    name: 'Needs you',
    open: async (page) => {
      await page.goto('/o/acme/needs-you');
      await expect(page.getByRole('heading', { name: 'Needs you' })).toBeVisible();
      await expect(page.getByRole('list', { name: /Waiting on you/ })).toBeVisible();
    },
  },
  {
    name: 'Item',
    open: async (page) => {
      await page.goto('/o/acme/f/goal/items/12');
      await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
    },
  },
  {
    name: 'Item trace',
    open: async (page) => {
      await page.goto('/o/acme/f/goal/items/12?tab=trace');
      await expect(page.getByRole('tabpanel', { name: 'Trace' })).toBeVisible();
      await expect(page.getByRole('list', { name: 'Legend' })).toBeVisible();
    },
  },
  {
    name: 'Item teardown',
    open: async (page) => {
      await page.goto('/o/acme/f/goal/items/4?tab=teardown');
      await expect(page.getByRole('tabpanel', { name: 'Teardown' })).toBeVisible();
      await expect(page.getByRole('region', { name: 'Totals' })).toBeVisible();
    },
  },
  {
    name: 'New trigger',
    open: async (page) => {
      await page.goto('/o/acme/f/software/intake/new');
      await expect(page.getByRole('dialog', { name: 'New trigger' })).toBeVisible();
    },
  },
  {
    name: 'Jonah',
    open: async (page) => {
      await page.goto('/o/acme/f/software/jonah');
      await expect(page.getByRole('heading', { level: 1, name: 'Jonah' })).toBeVisible();
    },
  },
  {
    name: 'Home',
    open: async (page) => {
      await page.goto('/o/acme');
      await expect(page.getByRole('heading', { level: 1, name: 'Home' })).toBeVisible();
    },
  },
  {
    name: 'Definition line',
    open: async (page) => {
      await page.goto('/o/acme/f/software/definition/line');
      await expect(page.getByRole('heading', { level: 1, name: 'Definition' })).toBeVisible();
      await expect(page.getByRole('list', { name: 'Stations, in order' })).toBeVisible();
    },
  },
  {
    name: 'Board',
    open: async (page) => {
      await page.goto('/o/acme/f/goal/board');
      await expect(page.getByRole('region', { name: 'Stations' })).toBeVisible();
      await page.waitForLoadState('networkidle');
    },
  },
  {
    name: 'Account',
    open: async (page) => {
      await page.goto('/account');
      await expect(page.getByRole('heading', { level: 1, name: 'Your account' })).toBeVisible();
      await expect(page.getByRole('button', { name: 'Save changes' }).filter({ visible: true })).toBeVisible();
    },
  },
  {
    name: 'Factory settings',
    open: async (page) => {
      await page.goto('/o/acme/f/goal/settings');
      await expect(page.getByRole('heading', { level: 1, name: 'Settings' })).toBeVisible();
    },
  },
  {
    name: 'Assembly item',
    open: async (page) => {
      await page.goto('/o/acme/f/goal/items/17');
      await expect(page.getByRole('heading', { name: 'Parts', exact: true })).toBeVisible();
    },
  },
  {
    name: 'Agents',
    open: async (page) => {
      await page.goto('/o/acme/f/goal/agents');
      await expect(page.getByRole('heading', { level: 1, name: 'Agents' })).toBeVisible();
    },
  },
  {
    name: 'Agent',
    open: async (page) => {
      await page.goto('/o/acme/f/goal/agents/reviewer');
      await expect(page.getByRole('heading', { level: 1, name: 'reviewer' })).toBeVisible();
    },
  },
  {
    name: 'Connections',
    open: async (page) => {
      await page.goto('/o/acme/connections');
      await expect(page.getByRole('heading', { level: 1, name: 'Connections' })).toBeVisible();
    },
  },
  {
    name: 'GitHub connection',
    open: async (page) => {
      await page.goto('/o/acme/connections/github');
      await expect(page.getByRole('heading', { level: 1, name: /github/i })).toBeVisible();
    },
  },
  {
    name: 'Knowledge source',
    open: async (page) => {
      // The mock has no source at first: create the upload source the way the New source page does.
      const created = await page.request.post('/v1/orgs/acme/knowledge/sources', {
        data: { name: 'handbook', title: 'Handbook', kind: 'upload' },
      });
      expect(created.status()).toBe(201);
      await page.goto('/o/acme/knowledge/handbook');
      await expect(page.getByRole('heading', { level: 1, name: /Handbook/ })).toBeVisible();
    },
  },
  {
    name: 'Spend',
    open: async (page) => {
      await page.goto('/o/acme/spend');
      await expect(page.getByRole('heading', { level: 1, name: 'Spend and billing' })).toBeVisible();
    },
  },
];

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

for (const scheme of ['light', 'dark'] as const) {
  for (const size of widths) {
    for (const entry of pages) {
      test(`${entry.name} does not scroll sideways at ${size.width}x${size.height} in ${scheme} mode`, async ({
        page,
      }, testInfo) => {
        // The matrix sets its own viewports, so it runs once, in the desktop project.
        test.skip(testInfo.project.name !== 'desktop-1440', 'one project runs the whole matrix');
        await page.setViewportSize(size);
        await page.emulateMedia({ colorScheme: scheme });
        await entry.open(page);
        await expectNoHorizontalScroll(page);
        await expectNoClippedElements(page);
        await testInfo.attach(`${entry.name}-${size.width}-${scheme}`, {
          body: await page.screenshot(),
          contentType: 'image/png',
        });
      });
    }
  }
}
