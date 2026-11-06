// Box helpers for the 390 px layout specs: a control's size, and whether two controls' boxes overlap.
import { expect, type Locator } from '@playwright/test';

/** The rendered box of the one element a locator names. */
export async function boxOf(locator: Locator) {
  await expect(locator).toBeVisible();
  const box = await locator.boundingBox();
  if (!box) throw new Error('the control has no box');
  return box;
}

/** The control's box is at least 44×44 px, the phone target MASTER.md asks for. */
export async function expectTarget44(locator: Locator, name: string) {
  const { width, height } = await boxOf(locator);
  expect(width, `${name} is ${width}px wide`).toBeGreaterThanOrEqual(44);
  expect(height, `${name} is ${height}px tall`).toBeGreaterThanOrEqual(44);
}

/** The two controls' boxes share no area. */
export async function expectNoOverlap(a: Locator, b: Locator, names: string) {
  const [x, y] = [await boxOf(a), await boxOf(b)];
  const overlaps = x.x < y.x + y.width && y.x < x.x + x.width && x.y < y.y + y.height && y.y < x.y + x.height;
  expect(overlaps, `${names} overlap`).toBe(false);
}

/**
 * The characters of each truncated title the page shows: the share of the text's width that fits,
 * times its length, or the whole text when it fits. `selector` finds the elements that truncate.
 */
export async function shownTitleCharacters(scope: Locator, selector: string) {
  return scope.evaluate(
    (root, sel) =>
      Array.from(root.querySelectorAll<HTMLElement>(sel))
        .filter((el) => el.getClientRects().length > 0)
        .map((el) => {
          const text = (el.textContent ?? '').trim();
          // The text's own width, from a Range: an inline link has no box width of its own, and its
          // cell clips it, so the clip edge is the nearer of the element's edge (when it is a box)
          // and the cell's content edge.
          const range = document.createRange();
          range.selectNodeContents(el);
          const textBox = range.getBoundingClientRect();
          const td = el.closest('td');
          let clipRight = Number.POSITIVE_INFINITY;
          if (td) clipRight = td.getBoundingClientRect().right - parseFloat(getComputedStyle(td).paddingRight);
          if (getComputedStyle(el).display !== 'inline')
            clipRight = Math.min(clipRight, el.getBoundingClientRect().right);
          const room = Math.max(0, clipRight - textBox.left);
          const shown = textBox.width > room ? Math.floor((room / textBox.width) * text.length) : text.length;
          return { text: text.slice(0, 40), shown, needed: Math.min(30, text.length) };
        }),
    selector,
  );
}

/**
 * Refs that are cut off. A ref is cut when its right edge passes the content edge of its cell, or
 * when the element itself overflows. Returns how many refs were found, so a selector that matches
 * nothing fails the test instead of passing it.
 */
export async function cutRefs(scope: Locator, selector = '[data-cell="ref"]') {
  return scope.evaluate((root, sel) => {
    const found = Array.from(root.querySelectorAll<HTMLElement>(sel)).filter((el) => el.getClientRects().length > 0);
    const cut = found
      .filter((el) => {
        if (el.scrollWidth > el.clientWidth) return true;
        const td = el.closest('td');
        if (!td || td === el) return false;
        const contentRight = td.getBoundingClientRect().right - parseFloat(getComputedStyle(td).paddingRight);
        return el.getBoundingClientRect().right > contentRight + 0.5;
      })
      .map((el) => el.textContent);
    return { count: found.length, cut };
  }, selector);
}
