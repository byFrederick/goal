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
    // Each figure is a term (what it counts) with its value as the definition.
    await expect(card.getByRole('term').filter({ hasText: /^Touchless: / })).toBeVisible();
    await expect(card.getByRole('definition').filter({ hasText: /^\d+%$/ })).toBeVisible();
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

test("the board says the week's touches per item, by station and on each touched card", async ({ page }) => {
  await touchSre8(page);
  await page.goto(isPhone(page) ? '/o/acme/f/sre/board?station=investigate' : '/o/acme/f/sre/board');

  if (isPhone(page)) {
    await expect(page.getByText(/^\d+\.\d touches per item$/)).toBeVisible();
    await expect(page.getByText(/^Investigate is the constraint: \d+ of \d+ busy, \d+ waiting$/)).toBeVisible();
    await expect(page.getByText(/^\d\.\d\d touches per item\.$/)).toBeVisible();
    await expect(page.getByRole('link', { name: 'By station' })).toHaveAttribute(
      'href',
      '/o/acme/f/sre#touches-summary',
    );
  } else {
    // The desktop board shows no touches (the canvas picture has none); the Floor keeps the touches panel.
    await expect(page.getByRole('link', { name: /touches per item/ })).toHaveCount(0);
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

test('two quick reason changes end on the second, never flipping back to the first', async ({ page, request }) => {
  await touchSre8(page);
  // The first answers after 500 ms, the second after 2.5 s: the first settles while the second is out.
  await request.post('/__mock/fail', { data: { path: '/touches/', method: 'PUT', delay_ms: 500 } });
  await request.post('/__mock/fail', { data: { path: '/touches/', method: 'PUT', delay_ms: 2500 } });
  await page.goto('/o/acme/f/sre/items/8');
  const reason = page.getByRole('combobox', { name: /^Reason for the .+ touch$/ });
  await expect(reason).toHaveValue('');

  const put = (n: number) => {
    let seen = 0;
    return page.waitForResponse((r) => r.url().includes('/touches/') && r.request().method() === 'PUT' && ++seen === n);
  };
  const first = put(1);
  const second = put(2);
  await reason.selectOption({ label: 'Missing knowledge' });
  await reason.selectOption({ label: 'Other' });
  await first;
  // What a refetch after the first would have put back shows by now, if it ran.
  await page.waitForTimeout(400);
  expect(await reason.inputValue()).toBe('other');
  await second;
  await expect(reason).toHaveValue('other');
  await page.reload();
  await expect(page.getByRole('combobox', { name: /^Reason for the .+ touch$/ })).toHaveValue('other');
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

test('the board and the floor work while the touch metrics fail, and the floor offers to try again', async ({
  page,
  request,
}) => {
  // Every try fails (times 0) until the faults are cleared: the app retries a 500 twice first.
  await request.post('/__mock/fail', { data: { path: '/metrics/touches', method: 'GET', status: 500, times: 0 } });
  await page.goto('/o/acme/f/sre/board');
  await expect(page.getByRole('heading', { level: 1, name: 'SRE board' })).toBeVisible();

  await page.goto('/o/acme/f/sre');
  await expect(page.getByRole('heading', { level: 1, name: 'Floor' })).toBeVisible();
  const card = page.getByRole('region', { name: 'Human touches' });
  await expect(card.getByText('Couldn’t load the human touches.')).toBeVisible({ timeout: 15_000 });
  await expectAccessible(page);
  await request.delete('/__mock/fail');
  await card.getByRole('button', { name: 'Try again' }).click();
  await expect(card.getByText(isPhone(page) ? /^\d+\.\d per item$/ : /^\d+\.\d touches per item$/)).toBeVisible();
});
