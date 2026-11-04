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
