// E10: a person moves, pauses and sends back work, at any state, without dragging (WCAG 2.2 2.5.7).
import { expect, test } from '@playwright/test';
import { expectAccessible, isPhone } from './support';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

const serveUI = 'goald: serve the embedded web UI'; // goal#12, waiting at Approve spec

test('move a card with the keyboard: focus it, press M, choose, confirm', async ({ page, request }) => {
  await page.goto('/o/acme/f/goal/board?station=approve-spec');
  const card = page.getByRole('link', { name: serveUI });
  await expect(card).toHaveAttribute('aria-keyshortcuts', 'M');

  await card.focus();
  await page.keyboard.press('m');

  const dialog = page.getByRole('dialog', { name: 'Move goal#12' });
  await expect(dialog).toBeVisible();
  // The next station down the line is chosen and focused; the arrow keys choose another.
  await expect(dialog.getByRole('radio', { name: /Build/ })).toBeFocused();
  await expect(dialog.getByRole('radio', { name: /Build/ })).toBeChecked();
  await expectAccessible(page);

  await page.keyboard.press('ArrowDown');
  await expect(dialog.getByRole('radio', { name: /Review/ })).toBeChecked();
  await page.keyboard.press('Tab'); // the reason
  await page.keyboard.press('Tab'); // Cancel
  await page.keyboard.press('Tab');
  await expect(dialog.getByRole('button', { name: 'Move to Review' })).toBeFocused();
  await page.keyboard.press('Enter');

  await expect(dialog).toBeHidden();
  await expect(page.getByText('Moved goal#12 to Review.')).toBeVisible();
  const item = await (await request.get('/v1/orgs/acme/items/goal%2312')).json();
  expect([item.station, item.state, item.rework_count]).toEqual(['review', 'queued', 0]);

  if (!isPhone(page)) {
    const review = page.getByRole('region', { name: 'Review', exact: true });
    await expect(review.getByRole('link', { name: serveUI })).toBeVisible();
    await expect(review.getByRole('link', { name: serveUI })).toBeFocused();
  }
});

test('moving upstream asks why, and the Move button opens the same menu', async ({ page, request }) => {
  await page.goto('/o/acme/f/goal/board?station=approve-spec');
  await page.getByRole('button', { name: 'Move goal#12 to another station' }).click();

  const dialog = page.getByRole('dialog', { name: 'Move goal#12' });
  await dialog.getByRole('radio', { name: /Spec/ }).check();
  await expect(dialog.getByText('Sends it back: counts as rework')).toHaveCount(2); // inbox and spec
  await dialog.getByRole('button', { name: 'Move to Spec' }).click();
  await expect(dialog.getByText('Say why it goes back; the agent reads it first.')).toBeVisible();

  await dialog.getByLabel('Why it goes back').fill('The spec misses the reload case.');
  await dialog.getByRole('button', { name: 'Move to Spec' }).click();

  await expect(page.getByText('Moved goal#12 to Spec.')).toBeVisible();
  const item = await (await request.get('/v1/orgs/acme/items/goal%2312')).json();
  expect([item.station, item.rework_count]).toEqual(['spec', 1]);
});

test('drag a card to another column, then confirm the move', async ({ page }) => {
  test.skip(isPhone(page), 'phones move cards with the arrow button');
  await page.goto('/o/acme/f/goal/board');
  const card = page.getByRole('link', { name: serveUI });
  const build = page.getByRole('region', { name: 'Build', exact: true });

  // Narrow boards scroll sideways: bring the target column in, then grab what shows of the card.
  await card.scrollIntoViewIfNeeded();
  await build.evaluate((column) => {
    const columns = column.closest<HTMLElement>('section[aria-label="Stations"]');
    if (!columns) return;
    const offset = column.getBoundingClientRect().left - columns.getBoundingClientRect().left;
    columns.scrollLeft += offset - columns.clientWidth / 2;
  });
  const from = await card.boundingBox();
  const to = await build.boundingBox();
  if (!from || !to) throw new Error('the card or the column has no box');
  const grab = { x: Math.max(from.x, 0) + 8, y: from.y + 5 };
  await page.mouse.move(grab.x, grab.y);
  await page.mouse.down();
  await page.mouse.move(grab.x + 20, grab.y + 10, { steps: 4 });
  await page.mouse.move(to.x + 30, to.y + 60, { steps: 8 });
  await page.mouse.up();

  const dialog = page.getByRole('dialog', { name: 'Move goal#12' });
  await expect(dialog.getByRole('radio', { name: /Build/ })).toBeChecked();
  await dialog.getByRole('button', { name: 'Move to Build' }).click();
  await expect(build.getByRole('link', { name: serveUI })).toBeVisible();
});

test('carry a card with the keyboard to a column off the screen: one arrow, one column', async ({ page }) => {
  test.skip(isPhone(page), 'phones move cards with the arrow button');
  await page.goto('/o/acme/f/goal/board');
  const merge = page.getByRole('region', { name: 'Merge', exact: true });
  await page.getByRole('link', { name: serveUI }).focus();
  // At 768 px the board scrolls sideways and Merge starts out of view.
  if (page.viewportSize()?.width === 768) await expect(merge).not.toBeInViewport();

  await page.keyboard.press('Space');
  await expect(page.getByText('goal#12 is over Approve spec.')).toBeAttached();
  // dnd-kit starts listening for the arrows in a task it queues at the pick-up: let it run.
  await page.evaluate(() => new Promise((resolve) => setTimeout(resolve)));

  // Three presses in a row, as a person taps: each carries the card one column, scrolling the
  // board at once when the column is out of view, so the next press starts from there.
  await page.keyboard.press('ArrowRight');
  await page.keyboard.press('ArrowRight');
  await page.keyboard.press('ArrowRight');
  await expect(page.getByText('goal#12 is over Merge.')).toBeAttached();
  await expect(merge).toBeInViewport();
  await page.keyboard.press('Space');

  const dialog = page.getByRole('dialog', { name: 'Move goal#12' });
  await expect(dialog.getByRole('radio', { name: /Merge/ })).toBeChecked();
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
});

test('pause a running item, then resume it', async ({ page }) => {
  await page.goto('/o/acme/f/goal/items/9');
  await page.getByRole('button', { name: 'Pause' }).click();

  const dialog = page.getByRole('dialog', { name: 'Pause goal#9?' });
  await expect(dialog.getByText(/The agent working on it now stops\./)).toBeVisible();
  await dialog.getByLabel('Why (optional)').fill('Waiting for the gateway fix.');
  await dialog.getByRole('button', { name: 'Pause' }).click();

  await expect(page.getByText('Paused goal#9 at Build.')).toBeVisible();
  await page.getByRole('button', { name: 'Resume' }).click();
  await expect(page.getByText('goal#9 is back in the queue.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Pause' })).toBeVisible();
});

test('send a running item back to its rework station', async ({ page, request }) => {
  await page.goto('/o/acme/f/goal/items/9');
  await page.getByRole('button', { name: 'Send back to Spec' }).click();

  const dialog = page.getByRole('dialog', { name: 'Send goal#9 back to Spec' });
  await expect(dialog.getByText(/The agent working on it now stops\./)).toBeVisible();
  await dialog.getByLabel('What must change').fill('Cache per agent, not per run.');
  await dialog.getByRole('button', { name: 'Send back' }).click();

  await expect(page.getByText('Sent goal#9 back. It is now at Spec.')).toBeVisible();
  const events = await (await request.get('/v1/orgs/acme/items/goal%239/events')).json();
  expect(events.events.map((e: { kind: string }) => e.kind)).toEqual(
    expect.arrayContaining(['lease_taken', 'rejected']),
  );
});

test('a station a publish adds is a Move choice on the open Item page, without navigating', async ({
  page,
  request,
}) => {
  await page.goto('/o/acme/f/goal/items/12');
  const move = page.getByRole('button', { name: 'Move', exact: true });
  await move.click();
  const dialog = page.getByRole('dialog', { name: 'Move goal#12' });
  await expect(dialog.getByRole('radio', { name: /Build/ })).toBeVisible();
  await expect(dialog.getByRole('radio', { name: /QA check/i })).toHaveCount(0);
  await page.keyboard.press('Escape');

  // Elsewhere (the CLI, another tab): a draft adds QA check before Done and is published.
  const factory = '/v1/orgs/acme/factories/goal';
  const running = await (await request.get(factory)).json();
  const draft = await (await request.get(`${factory}/draft/model`)).json();
  const stations = draft.model.line.stations as { id: string }[];
  const saved = await request.put(`${factory}/draft/model/line`, {
    data: {
      value: {
        ...draft.model.line,
        stations: [...stations.slice(0, -1), { id: 'qa-check', name: 'QA check', kind: 'human' }, stations.at(-1)],
      },
    },
    headers: { 'If-Match': `"${draft.etags.line}"` },
  });
  expect(saved.ok()).toBe(true);
  const published = await request.post(`${factory}/draft/publish`, {
    data: { revision: (await saved.json()).revision, expected_current_version_id: running.current.id },
  });
  expect(published.status()).toBe(201);

  await move.click();
  await expect(dialog.getByRole('radio', { name: /QA check/i })).toBeVisible();
});
