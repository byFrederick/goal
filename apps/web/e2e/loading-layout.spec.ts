// The loading states do not scroll sideways and keep the shell (MOTION.md, Loading; MASTER.md "Layout"). Each
// route's data is held with the mock fault (POST /__mock/fail, 20 s, every request), the URL opens with a full
// load and waits for the skeleton (aria-busy). Then the document is no wider than the window and, on a phone,
// the tab bar is there from the first paint. Holding every organization request keeps the page in the shell's
// own pending state, which draws the skeleton of the route being opened. Not run in the box (no browser).
import { expect, test } from '@playwright/test';
import { expectNoHorizontalScroll } from './support';

const widths = [
  { width: 1440, height: 1000 },
  { width: 768, height: 1024 },
  { width: 390, height: 844 },
];

const routes = [
  { name: 'Floor', url: '/o/acme/f/goal' },
  { name: 'Board', url: '/o/acme/f/goal/board' },
  { name: 'Items', url: '/o/acme/f/goal/items' },
  { name: 'Item', url: '/o/acme/f/goal/items/12' },
  { name: 'Runs', url: '/o/acme/runs' },
  { name: 'Agents', url: '/o/acme/f/goal/agents' },
  { name: 'Jonah', url: '/o/acme/f/goal/jonah' },
  { name: 'Secrets', url: '/o/acme/secrets' },
  { name: 'Secret', url: '/o/acme/secrets/github-token' },
  { name: 'Import secrets', url: '/o/acme/secrets/import' },
];

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

test.afterEach(async ({ request }) => {
  await request.delete('/__mock/fail');
});

for (const scheme of ['light', 'dark'] as const) {
  for (const size of widths) {
    for (const route of routes) {
      test(`${route.name} loading does not scroll sideways at ${size.width}x${size.height} in ${scheme} mode`, async ({
        page,
        request,
      }, testInfo) => {
        test.skip(testInfo.project.name !== 'desktop-1440', 'one project runs the whole matrix');
        await page.setViewportSize(size);
        await page.emulateMedia({ colorScheme: scheme });
        await request.post('/__mock/fail', { data: { path: '/orgs', delay_ms: 20000, times: 0 } });
        await page.goto(route.url, { waitUntil: 'commit' });
        await expect(page.locator('[aria-busy="true"]').first()).toBeAttached();
        await expectNoHorizontalScroll(page);
        if (size.width < 768) {
          await expect(page.getByRole('navigation', { name: 'Tabs' })).toBeVisible();
        }
      });
    }
  }
}
