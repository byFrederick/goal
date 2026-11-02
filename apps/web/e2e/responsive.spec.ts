// No page scrolls sideways (MASTER.md "Layout"): Runs, Items, Needs you, an item page, Intake's New trigger
// dialog and Jonah at 1280x800, 1024x768, 768x1024 and 390x844, in light and dark, against the mock. A wide table
// scrolls in its own box, so only the document's width is asserted. Each visit attaches a screenshot. Not run in
// the box (no browser): the pull request says so.
import { expect, type Page, test } from '@playwright/test';
import { expectNoHorizontalScroll } from './support';

const widths = [
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
        await testInfo.attach(`${entry.name}-${size.width}-${scheme}`, {
          body: await page.screenshot(),
          contentType: 'image/png',
        });
      });
    }
  }
}
