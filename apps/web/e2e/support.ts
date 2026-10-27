// What every flow checks: axe at WCAG 2.2 AA, no horizontal scroll, which chrome the width shows.
import AxeBuilder from '@axe-core/playwright';
import { expect, type Locator, type Page } from '@playwright/test';

export const isPhone = (page: Page) => (page.viewportSize()?.width ?? 0) < 768;

export async function expectAccessible(page: Page) {
  const { violations } = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'])
    .analyze();
  expect(violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).join(', ')}`)).toEqual([]);
}

export async function expectNoHorizontalScroll(page: Page) {
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  expect(overflow).toBeLessThanOrEqual(0);
}

/** The sidebar from 768 px, the top row below: the same landmark, one of them shown. */
export const orgNav = (page: Page) => page.getByRole('navigation', { name: 'Organization and factories' });

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
