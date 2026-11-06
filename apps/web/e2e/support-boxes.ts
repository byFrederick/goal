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
          const shown =
            el.scrollWidth > el.clientWidth ? Math.floor((el.clientWidth / el.scrollWidth) * text.length) : text.length;
          return { text: text.slice(0, 40), shown, needed: Math.min(30, text.length) };
        }),
    selector,
  );
}

/** Refs whose text is wider than their box (cut off), found by the mono ref spans of a table. */
export async function cutRefs(scope: Locator, selector = 'span.font-mono') {
  return scope.evaluate(
    (root, sel) =>
      Array.from(root.querySelectorAll<HTMLElement>(sel))
        .filter((el) => el.getClientRects().length > 0 && el.scrollWidth > el.clientWidth)
        .map((el) => el.textContent),
    selector,
  );
}
