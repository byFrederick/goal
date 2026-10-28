import { expect, test } from '@playwright/test';
import { expectNoHorizontalScroll, orgNav } from './support';

// The desktop sidebar's resize handle at 1440 px (MASTER.md "The shell"): drag, snap to the rail, keyboard,
// saved width, and no overflow at the narrowest and widest widths. Not run in the build box: no browser there.
test.use({ viewport: { width: 1440, height: 900 } });

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

const handle = (page: import('@playwright/test').Page) => page.getByRole('separator', { name: 'Resize sidebar' });

/** Drags the handle from its current x to `to`, in pixels, and releases it. */
async function dragTo(page: import('@playwright/test').Page, to: number) {
  const box = await handle(page).boundingBox();
  if (!box) throw new Error('the resize handle has no box');
  const x = box.x + box.width / 2;
  const y = box.y + box.height / 2;
  await page.mouse.move(x, y);
  await page.mouse.down();
  await page.mouse.move(to, y, { steps: 8 });
  await page.mouse.up();
}

const sidebarWidth = (page: import('@playwright/test').Page) =>
  orgNav(page).evaluate((el) => el.parentElement?.getBoundingClientRect().width ?? 0);

test('the sidebar resizes by dragging, keeps its width after a reload, and snaps to the rail', async ({ page }) => {
  const errors: string[] = [];
  page.on('console', (m) => {
    if (m.type() === 'error') errors.push(m.text());
  });

  await page.goto('/o/acme/f/goal');
  await expect(orgNav(page)).toBeVisible();
  expect(await sidebarWidth(page)).toBe(256);

  await dragTo(page, 320);
  await expect.poll(() => sidebarWidth(page)).toBe(320);
  await page.reload();
  await expect.poll(() => sidebarWidth(page)).toBe(320);

  // Dragging below 160 px and releasing shows the rail: icons only, no label text.
  await dragTo(page, 120);
  const rail = orgNav(page);
  await expect(page.getByRole('button', { name: 'Expand sidebar' })).toBeVisible();
  await expect.poll(() => sidebarWidth(page)).toBe(60);
  await expect(rail.getByText('Floor', { exact: true })).toHaveCount(0);

  // Dragging the rail's edge out past 160 px expands it to the dragged width.
  await dragTo(page, 300);
  await expect.poll(() => sidebarWidth(page)).toBe(300);

  // Double-click resets to 256 px.
  await handle(page).dblclick();
  await expect.poll(() => sidebarWidth(page)).toBe(256);

  expect(errors).toEqual([]);
});

test('the keyboard steps the width, and Home collapses it', async ({ page }) => {
  await page.goto('/o/acme/f/goal');
  await handle(page).focus();
  await page.keyboard.press('ArrowRight');
  await page.keyboard.press('ArrowRight');
  await expect(handle(page)).toHaveAttribute('aria-valuenow', '288');
  await expect.poll(() => sidebarWidth(page)).toBe(288);

  await page.keyboard.press('Home');
  await expect(page.getByRole('button', { name: 'Expand sidebar' })).toBeVisible();
});

test('no element inside the sidebar overflows at 216 px and at 400 px', async ({ page }) => {
  await page.goto('/o/acme/f/goal');
  for (const width of [216, 400]) {
    await dragTo(page, width);
    await expect.poll(() => sidebarWidth(page)).toBe(width);

    const overflowing = await orgNav(page).evaluate((nav) =>
      Array.from(nav.querySelectorAll('*'))
        .filter((el) => el.scrollWidth > el.clientWidth)
        .map((el) => `${el.tagName}.${el.className}`),
    );
    expect(overflowing, `width ${width}`).toEqual([]);
    await expectNoHorizontalScroll(page);
  }
});

test('the Board fills the viewport beside the sidebar at every width', async ({ page }) => {
  await page.goto('/o/acme/f/goal/board');
  for (const to of [216, 400]) {
    await dragTo(page, to);
    await expect.poll(() => sidebarWidth(page)).toBe(to);
    await expectNoHorizontalScroll(page);
  }
});
