// Human touches per item (spec 4 §4.9, §4.10, §10): the Floor's headline and panel, the factory
// tiles, the Board's figures and card counts, and the Item's touches with their reason select.
import { expect, type Page, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll, isPhone } from './support';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

/** A person's note on sre#8: one touch, with no reason yet. */
async function touchSre8(page: Page) {
  const res = await page.request.post('/v1/orgs/acme/items/sre%238/notes', {
    data: { text: 'Also check the connection pool' },
  });
  expect(res.ok()).toBe(true);
}

test('the floor says touches per finished item and where they happen', async ({ page }) => {
  await page.goto('/o/acme/f/sre');
  const card = page.getByRole('region', { name: 'Human touches' });
  await expect(card).toBeVisible();

  if (isPhone(page)) {
    await expect(card.getByText(/^\d+\.\d per item$/)).toBeVisible();
    await expect(card.getByText(/vs last week, fewer is better$/)).toBeVisible();
    await expect(card.getByText('Touchless items')).toBeVisible();
    await expect(card.getByText(/^Most touches: /)).toBeVisible();
  } else {
    await expect(card.getByText(/^\d+\.\d touches per item$/)).toBeVisible();
    await expect(card.getByText(/^vs last week\. \d+ touches on \d+ finished items$/)).toBeVisible();
    await expect(card.getByText(/^Touchless: \d+ of \d+ items finished with no human touch$/)).toBeVisible();
    await expect(card.getByText(/^per item, plus \d+\.\d planned/)).toBeVisible();

    const where = card.getByRole('region', { name: 'Where touches happen' });
    await expect(where.getByRole('img', { name: /^Approve: \d+ touches, \d\.\d\d per item$/ })).toBeVisible();
    await where.getByText('Kind', { exact: true }).click();
    await expect(page).toHaveURL(/\?touches=kind$/);
    await expect(where.getByRole('img', { name: /^Approved: \d+ touch(es)?, / })).toBeVisible();
    await where.getByText('Version', { exact: true }).click();
    await expect(where.getByRole('img', { name: /^v\d+: / }).first()).toBeVisible();
    await expect(where.getByText(/^\d+ items, \d\.\d per item, \d+% touchless$/).first()).toBeVisible();
    await expect(where.getByRole('link', { name: 'See the finished items' })).toHaveAttribute(
      'href',
      '/o/acme/f/sre/items?state=done',
    );
  }
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('each factory tile says its touches per item this week against last week', async ({ page }) => {
  await page.goto('/o/acme');
  const sre = page.getByRole('article', { name: 'sre' });
  // From 768 px a tile above the stats, on a phone a line under them: one of them shows.
  await expect(sre.getByText(/human touches per item/).filter({ visible: true })).toBeVisible();
  await expect(sre.getByText(/\d+% touchless/).filter({ visible: true })).toBeVisible();
  // The support line finished nothing this week: no per item to say.
  const support = page.getByRole('article', { name: 'support' });
  await expect(support.getByText(/^No items finished this week/).filter({ visible: true })).toBeVisible();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test("the board says the week's touches per item, by station and on each touched card", async ({ page }) => {
  await touchSre8(page);
  await page.goto(isPhone(page) ? '/o/acme/f/sre/board?station=investigate' : '/o/acme/f/sre/board');

  if (isPhone(page)) {
    await expect(page.getByText(/^\d+\.\d touches per item$/)).toBeVisible();
    await expect(page.getByText(/^The constraint: .+\. \d\.\d\d touches per item\.$/)).toBeVisible();
    await expect(page.getByRole('link', { name: 'By station' })).toHaveAttribute(
      'href',
      '/o/acme/f/sre#touches-summary',
    );
  } else {
    await expect(page.getByRole('link', { name: /^\d+\.\d touches per item ?, \d+% touchless$/ })).toHaveAttribute(
      'href',
      '/o/acme/f/sre#touches',
    );
    const stations = page.getByRole('region', { name: 'Stations in line order' });
    await expect(stations.getByText(/^\d\.\d\d touches per item$/)).toHaveCount(5);
  }
  const card = page.locator('[data-card]').filter({ hasText: 'sre#8' }).filter({ visible: true });
  await expect(card.getByText('1 touch', { exact: true })).toBeVisible();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('the item lists its touches, and a reason set on one stays', async ({ page }) => {
  await touchSre8(page);
  await page.goto('/o/acme/f/sre/items/8');

  const card = page.getByRole('region', { name: 'Human touches' });
  await expect(card.getByText('1 so far')).toBeVisible();
  await expect(card.getByText(/^sre averages \d+\.\d per item\. Set a reason/)).toBeVisible();
  await expect(card.getByText(/Local user left a note: “Also check the connection pool”\.$/)).toBeVisible();
  await expect(card.getByText(/^Investigate, (note, )?unplanned$/)).toBeVisible();
  if (!isPhone(page)) await expect(page.getByText('1 human touch', { exact: true })).toBeVisible();

  const reason = card.getByRole('combobox', { name: /^Reason for the .+ touch$/ });
  await expect(reason).toHaveValue('');
  await expectAccessible(page);

  const saved = page.waitForResponse((r) => r.url().includes('/touches/') && r.request().method() === 'PUT');
  await reason.selectOption({ label: 'Missing knowledge' });
  expect((await saved).status()).toBe(204);

  await page.reload();
  await expect(page.getByRole('combobox', { name: /^Reason for the .+ touch$/ })).toHaveValue('missing_knowledge');
  await expectNoHorizontalScroll(page);
});

test('a refused reason says why and goes back to what goald has', async ({ page, request }) => {
  await touchSre8(page);
  await request.post('/__mock/fail', {
    data: { path: '/touches/', method: 'PUT', status: 404, code: 'touch-not-found' },
  });
  await page.goto('/o/acme/f/sre/items/8');

  const reason = page.getByRole('combobox', { name: /^Reason for the .+ touch$/ });
  await reason.selectOption({ label: 'Other' });
  await expect(page.getByRole('alert').filter({ hasText: /^Not saved: / })).toBeVisible();
  await expect(reason).toHaveValue('');
});
