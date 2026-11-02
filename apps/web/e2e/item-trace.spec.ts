// The item's Trace tab (ADR 0019, decision 2): opening it, "Full trace" on the Latest steps card, and the
// empty state. Item 6 has several runs in the mock; item 15 has none.
import { expect, test } from '@playwright/test';
import { expectNoClippedElements, expectNoHorizontalScroll } from './support';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

test('the Trace tab lists every run oldest first and shows the selected step', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/o/acme/f/goal/items/6');
  await page.getByRole('tab', { name: 'Trace' }).click();

  await expect(page).toHaveURL(/tab=trace/);
  const panel = page.getByRole('tabpanel', { name: 'Trace' });
  await expect(panel.getByRole('list', { name: 'Legend' })).toBeVisible();
  const runs = panel.getByRole('region');
  expect(await runs.count()).toBeGreaterThan(1);
  await expect(panel.getByRole('complementary')).toBeVisible();

  await panel.getByRole('button', { name: /^Read/ }).first().click();
  await expect(panel.getByRole('complementary').getByRole('heading', { name: 'Read' })).toBeVisible();
  await expectNoHorizontalScroll(page);
});

test('Full trace on the Latest steps card opens the Trace tab', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/o/acme/f/goal/items/6');
  await page.getByRole('button', { name: /^All \d+ events$/ }).click();

  await expect(page).toHaveURL(/tab=trace/);
  await expect(page.getByRole('tabpanel', { name: 'Trace' })).toBeVisible();
  await expectNoHorizontalScroll(page);
});

test('an item with no runs shows the empty state', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/o/acme/f/goal/items/15?tab=trace');

  await expect(page.getByText('No tool calls yet.')).toBeVisible();
});

test('Watch live opens the Trace tab and keeps a new step in view', async ({ page, request }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/o/acme/f/goal/items/9');
  await page.getByRole('button', { name: 'Watch live' }).click();

  await expect(page).toHaveURL(/tab=trace/);
  const panel = page.getByRole('tabpanel', { name: 'Trace' });
  await expect(panel.getByRole('list', { name: 'Legend' })).toBeVisible();
  // The first press already scrolls to the newest existing step, before any new one arrives.
  await expect(panel.getByRole('button', { name: /Grep\s*watch-live-marker/ })).toHaveCount(0);
  await expect(panel.getByRole('button').last()).toBeInViewport();

  await request.post('/__mock/step', { data: { number: 9, tool: 'Grep', input: 'watch-live-marker' } });
  const added = panel.getByRole('button', { name: /Grep\s*watch-live-marker/ });
  await expect(added).toBeVisible();
  await expect(added).toBeInViewport();
});

test('the Trace tab fits at every width', async ({ page }) => {
  for (const width of [1440, 1280, 1024, 768, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/o/acme/f/goal/items/6?tab=trace');
    await expect(page.getByRole('tabpanel', { name: 'Trace' }).getByRole('list', { name: 'Legend' })).toBeVisible();
    await expectNoHorizontalScroll(page);
    await expectNoClippedElements(page);
  }
});

for (const width of [1024, 1440]) {
  test(`every Took cell stays on one line at ${width}`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/o/acme/f/goal/items/6?tab=trace');
    await expect(page.getByRole('tabpanel', { name: 'Trace' })).toBeVisible();
    const wrapped = await page.evaluate(() =>
      Array.from(document.querySelectorAll<HTMLElement>('[role="tabpanel"] span.whitespace-nowrap.tabular-nums'))
        .filter((cell) => {
          const row = cell.closest('[role="row"], li') ?? cell.parentElement!;
          return (
            cell.scrollHeight > parseFloat(getComputedStyle(cell).lineHeight) + 1 ||
            cell.scrollWidth > cell.clientWidth ||
            cell.getBoundingClientRect().right > row.getBoundingClientRect().right + 0.5
          );
        })
        .map((cell) => cell.textContent),
    );
    expect(wrapped).toEqual([]);
  });

  test(`the Took column fits the longest live value at ${width}`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/o/acme/f/goal/items/6?tab=trace');
    await expect(page.getByRole('tabpanel', { name: 'Trace' })).toBeVisible();
    // The widest value a running span shows is an hour-scale "so far" duration.
    const room = await page.evaluate(() => {
      const cell = document.querySelector<HTMLElement>('[role="tabpanel"] span.whitespace-nowrap.tabular-nums');
      if (!cell) return null;
      const style = getComputedStyle(cell);
      const probe = document.createElement('span');
      probe.textContent = '12 h 41 min so far';
      probe.style.cssText = `position:absolute;visibility:hidden;white-space:nowrap;font-family:${style.fontFamily};font-size:${style.fontSize};font-weight:${style.fontWeight};font-variant-numeric:tabular-nums`;
      document.body.append(probe);
      const need = probe.getBoundingClientRect().width;
      probe.remove();
      const box = cell.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight);
      return { need, box };
    });
    expect(room).not.toBeNull();
    expect(room!.box).toBeGreaterThanOrEqual(room!.need);
  });
}
