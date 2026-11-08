// Menus and popovers have a visible edge in both themes: a white ring at 8% on dark, an ink ring at 6% on light.
import { expect, type Locator, test } from '@playwright/test';

const ring = { light: 'rgba(23, 23, 23, 0.06)', dark: 'rgba(255, 255, 255, 0.08)' } as const;
const repo = 'github.com/acme/app';
const waiting = {
  kind: 'ci',
  items: [],
  files: [],
  needs: [],
  ci: {
    commits: [{ repo, sha: '0123456789abcdef', url: 'https://github.com/acme/app/pull/7' }],
    checks: [{ repo, failed_jobs: [], name: 'build', state: 'running', url: 'https://ci.example.com/build' }],
    finished: 9,
    total: 14,
  },
};

async function expectRing(panel: Locator, scheme: 'light' | 'dark') {
  const shadow = await panel.evaluate((el) => getComputedStyle(el).boxShadow);
  expect(shadow.startsWith(ring[scheme]), shadow).toBe(true);
}

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

for (const width of [375, 1440]) {
  for (const scheme of ['light', 'dark'] as const) {
    test.describe(`${width} px, ${scheme}`, () => {
      test.use({ viewport: { width, height: 900 } });

      test.beforeEach(async ({ page }) => {
        await page.emulateMedia({ colorScheme: scheme });
        await page.route(/\/v1\/orgs\/[^/]+\/(factories\/[^/]+\/)?items\/[^/?]+(\?|$)/, async (route) => {
          const response = await route.fetch();
          const item = await response.json();
          Object.assign(item, { state: 'queued', waiting });
          await route.fulfill({ response, json: item });
        });
        await page.goto('/o/acme/f/goal/items/12');
      });

      test('the CI popover has its ring', async ({ page }) => {
        await page.getByRole('button', { name: 'About CI' }).filter({ visible: true }).first().click();
        await expectRing(page.getByRole('dialog').first(), scheme);
      });

      test('the More actions menu has its ring', async ({ page }) => {
        await page.getByRole('button', { name: 'More actions' }).filter({ visible: true }).first().click();
        await expectRing(page.getByRole('menu'), scheme);
      });
    });
  }
}
