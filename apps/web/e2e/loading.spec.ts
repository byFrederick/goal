// Loads that outlive the page that started them (a fetch whose only observer unmounts is
// cancelled, and the next page's loader that shares it must load again, not fail), and lists
// cached before an organization or factory was made elsewhere.
import { expect, type Page, test } from '@playwright/test';
import { openNavigation, orgMarkOf } from './support';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

test('a page whose data the page left was still loading loads it again instead of failing', async ({
  page,
  request,
}) => {
  // The Line section reads the organization's settings (slot limits); its first read is slow.
  await request.post('/__mock/fail', { data: { path: '/settings', method: 'GET', delay_ms: 3000 } });
  await page.goto('/o/acme/f/goal/definition/line');
  await expect(page.getByRole('heading', { level: 2, name: 'Line', exact: true })).toBeVisible();

  // Organization settings shares that read; leaving the Line section cancels it.
  // The organization menu at every width; on a phone it opens from the drawer's top row.
  await openNavigation(page);
  await orgMarkOf(page).click();
  await page.getByRole('menu').getByRole('menuitem', { name: 'Organization settings' }).click();

  await expect(page.getByRole('heading', { level: 1, name: 'Organization settings' })).toBeVisible();
  await expect(page.getByLabel('Name')).toHaveValue('Acme');
  await expect(page.getByText('goald is not answering')).toHaveCount(0);
});

/** Opens a path inside the running app (a link from elsewhere in it), without reloading the page. */
async function openInApp(page: Page, path: string) {
  await page.evaluate((to) => {
    history.pushState(null, '', to);
    dispatchEvent(new PopStateEvent('popstate'));
  }, path);
}

test('an organization and a factory made elsewhere open at once, not as Not found', async ({ page, request }) => {
  await page.goto('/o/acme');
  await expect(page.getByRole('heading', { level: 1, name: 'Factories' })).toBeVisible();

  // The CLI or another tab: the cached organization and factory lists don't have them yet.
  await request.post('/v1/orgs', { data: { name: 'Empty Co', slug: 'empty-co' } });
  await openInApp(page, '/o/empty-co');
  await expect(page.getByRole('heading', { level: 2, name: 'No factories in Empty Co yet' })).toBeVisible();

  await page.goto('/o/acme/f/goal');
  await expect(page.getByRole('heading', { level: 1, name: 'Goal floor' })).toBeVisible();
  const created = await request.post('/v1/orgs/acme/factories', {
    data: { name: 'triage', title: 'Triage', start: { kind: 'blank' } },
  });
  expect(created.ok()).toBe(true);
  await openInApp(page, '/o/acme/f/triage/definition');
  await expect(page.getByRole('heading', { level: 1, name: 'Definition' })).toBeVisible();
  await expect(page.getByText('No such factory')).toHaveCount(0);
});

test('a section whose code fails to arrive once is asked for again, and its picker opens focused', async ({ page }) => {
  // One lost response for the Agents section's chunk (a dropped connection): React.lazy alone
  // keeps that failure for good, and the section stayed on "goald is not answering".
  let dropped = false;
  await page.route(/\/assets\/agents-section-[\w-]+\.js$/, (route) => {
    if (dropped) return route.continue();
    dropped = true;
    return route.abort('connectionreset');
  });
  await page.goto('/o/acme/f/goal/definition/agents?agent=reviewer&add=skill');
  await expect(page.getByRole('combobox', { name: 'Add a skill from the catalog' })).toBeFocused();
  expect(dropped).toBe(true);
  await expect(page.getByText('goald is not answering')).toHaveCount(0);
});
