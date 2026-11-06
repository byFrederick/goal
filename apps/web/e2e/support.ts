// What every flow checks: axe at WCAG 2.2 AA, no horizontal scroll, nothing clipped past the right edge, which chrome the width shows.
import AxeBuilder from '@axe-core/playwright';
import { expect, type Locator, type Page } from '@playwright/test';

export const isPhone = (page: Page) => (page.viewportSize()?.width ?? 0) < 768;

export async function expectAccessible(page: Page) {
  // Axe reads the colour a frame paints. Wait until no route still shows its skeleton (rows mount and fade in
  // after it), then until finite entrance fades have ended on two checks in a row, or axe measures text mid-fade.
  // Looping animations (the skeleton pulse) never finish and are left alone.
  await expect(page.locator('[aria-busy="true"]')).toHaveCount(0);
  // Sonner removes a toast after 4 s, and the removal fades it to 1.03:1 (#e4e4e4 on #e0e0e0, run 42096576406's
  // trace; run 42136073943 failed the same way after a wait for "no toast leaving"). A slow runner can let that
  // timer fire while axe is scanning, after any wait has passed. Hovering a toast pauses sonner's timers, so a
  // toast on screen stays at rest for the whole scan. Toasts stay in the scan; the pointer leaves afterwards.
  const toast = page.locator('[data-sonner-toast]:not([data-removed="true"])').first();
  const box = (await toast.count()) > 0 ? await toast.boundingBox({ timeout: 1000 }).catch(() => null) : null;
  if (box) await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  // The view-transition wait is a guard: a transition paints the page from snapshots.
  await expect
    .poll(() =>
      page.evaluate(() => {
        const doc = document as Document & { activeViewTransition?: unknown };
        const transitioning =
          doc.activeViewTransition !== undefined
            ? doc.activeViewTransition !== null
            : document.documentElement.matches(':active-view-transition');
        const toastMoving = document.querySelectorAll(
          '[data-sonner-toast][data-mounted="false"], [data-sonner-toast][data-removed="true"]',
        ).length;
        return !transitioning && toastMoving === 0;
      }),
    )
    .toBe(true);
  const settled = () =>
    page.evaluate(() =>
      document
        .getAnimations()
        .every((a) => a.playState !== 'running' || a.effect?.getComputedTiming().iterations === Infinity),
    );
  await expect
    .poll(async () => (await settled()) && (await page.waitForTimeout(100), await settled()), { timeout: 10_000 })
    .toBe(true);
  const { violations } = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'])
    .analyze();
  if (box) await page.mouse.move(0, 0);
  expect(violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).join(', ')}`)).toEqual([]);
}

/**
 * Review, and any route view, has finished loading: its heading is shown and no skeleton (aria-busy) is
 * left in the main region. Assert the screen's content after this with the default timeout, not a bare wait
 * for a cold build, draft and lazy chunk on a saturated runner.
 */
export async function expectViewLoaded(page: Page) {
  const main = page.locator('#main');
  await expect(main.locator('[data-route-focus]').first()).toBeVisible({ timeout: 30_000 });
  await expect(main.locator('[aria-busy="true"]')).toHaveCount(0, { timeout: 30_000 });
}

/**
 * Every finite animation and transition has ended. axe reads the colours as drawn, so a page caught in
 * its skeleton cross-fade (MOTION.md, Loading) reads as low contrast; wait for it before expectAccessible.
 */
export async function expectSettled(page: Page) {
  // Three frames in a row with nothing running or pending: a transition that starts a frame after the
  // data lands (a skeleton reveal, a row entering) would otherwise slip between two checks.
  await page.waitForFunction(() => {
    const w = window as unknown as { __settledFrames?: number; __settledPolling?: boolean };
    const busy = document
      .getAnimations()
      .some(
        (a) => a.playState !== 'finished' && a.playState !== 'idle' && a.effect?.getTiming().iterations !== Infinity,
      );
    w.__settledFrames = busy ? 0 : (w.__settledFrames ?? 0);
    if (!w.__settledPolling) {
      w.__settledPolling = true;
      const tick = () => {
        const stillBusy = document
          .getAnimations()
          .some(
            (a) =>
              a.playState !== 'finished' && a.playState !== 'idle' && a.effect?.getTiming().iterations !== Infinity,
          );
        w.__settledFrames = stillBusy ? 0 : (w.__settledFrames ?? 0) + 1;
        requestAnimationFrame(tick);
      };
      requestAnimationFrame(tick);
    }
    return !busy && (w.__settledFrames ?? 0) >= 3;
  });
}

/**
 * WCAG 2.5.3 (label in name): every control's accessible name contains its visible text. axe tags the rule
 * experimental, so the tag run above skips it; this runs it by id. It is not part of expectAccessible.
 */
export async function expectLabelInName(page: Page) {
  const { violations } = await new AxeBuilder({ page }).withRules(['label-content-name-mismatch']).analyze();
  expect(violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).join(', ')}`)).toEqual([]);
}

/**
 * The document is never wider than the layout viewport: fails with the overflow in px. It compares with
 * `clientWidth`, not `innerWidth`: on a phone the layout viewport grows to fit an overflow (a button's hit
 * area, a pseudo-element), and innerWidth grows with it.
 */
export async function expectNoHorizontalScroll(page: Page) {
  const { scrollWidth, clientWidth } = await page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
  }));
  expect(
    scrollWidth - clientWidth,
    `page overflows the viewport by ${scrollWidth - clientWidth}px (scrollWidth ${scrollWidth}, clientWidth ${clientWidth})`,
  ).toBeLessThanOrEqual(0);
}

/**
 * No visible element reaches past the right edge of the window, outside a horizontal scroller. The shell
 * clips horizontal overflow, so scrollWidth no longer shows content that is cut off. Fails naming each
 * element and its overflow in px. Skips what is not drawn (hidden, zero-size, sr-only, aria-hidden) and
 * what sits inside an ancestor that scrolls horizontally (overflow-x auto or scroll).
 */
export async function expectNoClippedElements(page: Page) {
  const clipped = await page.evaluate(() => {
    const edge = document.documentElement.clientWidth;
    const label = (el: Element) => {
      const name = el.getAttribute('aria-label') ?? (el.textContent ?? '').trim().slice(0, 30);
      return `${el.tagName.toLowerCase()}${el.id ? `#${el.id}` : ''} "${name}"`;
    };
    const scrolls = (el: Element | null) => {
      for (let node = el; node && node !== document.body; node = node.parentElement) {
        const x = getComputedStyle(node).overflowX;
        if (x === 'auto' || x === 'scroll') return true;
      }
      return false;
    };
    const found: string[] = [];
    for (const el of document.body.querySelectorAll('*')) {
      const box = el.getBoundingClientRect();
      if (box.width === 0 || box.height === 0 || box.right <= edge + 0.5) continue;
      const style = getComputedStyle(el);
      if (style.visibility === 'hidden' || style.display === 'none' || Number(style.opacity) === 0) continue;
      if (el.closest('[aria-hidden="true"], [inert], .sr-only')) continue;
      // Monaco lays its text out in a 16,777,216 px wide layer that its editor box clips: what a person
      // sees ends at the editor, so an editor inside the window is not past its edge.
      const editor = el.closest('.monaco-editor');
      if (editor && editor.getBoundingClientRect().right <= edge + 0.5) continue;
      if (style.position === 'fixed' && box.left >= edge) continue;
      if (scrolls(el.parentElement) || scrolls(el)) continue;
      found.push(`${label(el)} ends ${Math.round((box.right - edge) * 10) / 10}px past the right edge`);
    }
    return found.slice(0, 10);
  });
  expect(clipped, 'elements past the right edge of the window').toEqual([]);
}

/** The sidebar from 768 px, the drawer below (open): the same landmark, one of them shown. */
export const orgNav = (page: Page) => page.getByRole('navigation', { name: 'Organization and factories' });

/**
 * Opens the phone's drawer (the Goal mark's "Open menu") when it is not open, so the navigation is
 * shown. From 768 px the sidebar is always shown, so this does nothing there.
 */
export async function openNavigation(page: Page) {
  if (!isPhone(page)) return;
  if ((await orgNav(page).filter({ visible: true }).count()) > 0) return;
  await page.getByRole('button', { name: 'Open menu' }).click();
  await expect(orgNav(page).filter({ visible: true })).toBeVisible();
}

/**
 * Waits until a factory's board follows the live stream: once the stream is connected its line
 * status says only that the line runs (with its WIP on a phone).
 */
export async function expectBoardLive(page: Page) {
  const status = page.getByRole('status').filter({ hasText: /^Line running(, WIP \d+( of \d+)?)?$/ });
  await expect(status.filter({ visible: true })).toBeVisible();
}

declare global {
  interface Window {
    /** The text of every toast shown since the page loaded (recordToasts). */
    __toasts?: string[];
  }
}

/**
 * Records the text of every toast as it appears. Sonner removes a toast after 4 s, which a slow
 * run can outlast between the action and its assertion; expectToast asserts on this record, not
 * on a toast that may be gone. Call it before the page loads.
 */
export async function recordToasts(page: Page) {
  await page.addInitScript(() => {
    const seen: string[] = [];
    window.__toasts = seen;
    new MutationObserver((records) => {
      for (const record of records) {
        for (const node of record.addedNodes) {
          if (!(node instanceof HTMLElement)) continue;
          const toasts = node.matches('[data-sonner-toast]')
            ? [node]
            : [...node.querySelectorAll<HTMLElement>('[data-sonner-toast]')];
          for (const toast of toasts) seen.push(toast.textContent ?? '');
        }
      }
    }).observe(document, { childList: true, subtree: true });
  });
}

/** A toast saying `text` was shown since the page loaded (recordToasts), however long ago. */
export async function expectToast(page: Page, text: string) {
  await expect.poll(() => page.evaluate(() => window.__toasts ?? [])).toContainEqual(expect.stringContaining(text));
}

/**
 * The first arrow of a keyboard drag: dnd-kit starts listening for the arrows in a task it queues
 * at the pick-up, so a press right after it can go unheard. The key is pressed again only while
 * `before` (where the pick-up left the item) still shows, until `after` shows.
 */
export async function pressUntilHeard(page: Page, key: string, before: Locator, after: Locator) {
  await expect
    .poll(async () => {
      if ((await after.count()) > 0) return true;
      if ((await before.count()) > 0) await page.keyboard.press(key);
      return (await after.count()) > 0;
    })
    .toBe(true);
}

/**
 * The organization's menu trigger of the shown navigation: the sidebar's name or the top row's mark
 * (which says the same name after its letters). It opens the organization menu or sheet.
 */
export const orgMarkOf = (page: Page, org = 'Acme') =>
  orgNav(page)
    .filter({ visible: true })
    .getByRole('button', { name: new RegExp(`${org} organization, switch organization`) });

const longText = ' with a deliberately long wording that would widen a table that does not cut it off'.repeat(3);

/** Makes every title and error in the JSON the page loads from `url` very long, so a table that lets its text set its width overflows. */
export async function lengthenTitles(page: Page, url: RegExp) {
  const grow = (value: unknown): unknown => {
    if (Array.isArray(value)) return value.map(grow);
    if (value && typeof value === 'object') {
      return Object.fromEntries(
        Object.entries(value).map(([key, inner]) => [
          key,
          (key === 'title' || key === 'error') && typeof inner === 'string' && inner ? inner + longText : grow(inner),
        ]),
      );
    }
    return value;
  };
  await page.route(url, async (route) => {
    const response = await route.fetch();
    if (!response.headers()['content-type']?.includes('json')) return route.fulfill({ response });
    return route.fulfill({ response, json: grow(await response.json()) });
  });
}
