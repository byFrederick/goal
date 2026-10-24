// Reduced motion (every flow runs with it): a change of state shows its final look at once.
import { expect, test } from '@playwright/test';

test('with reduced motion a style change does not transition', async ({ page }) => {
  await page.goto('/o/acme/f/goal/board');
  await expect(page.getByRole('heading', { level: 1, name: 'Goal board' })).toBeVisible();

  // An element with no transition of its own, changed within one task: no frame can pass, so a
  // transition would still show the old colour.
  const shown = await page.evaluate(() => {
    const el = document.createElement('span');
    el.textContent = 'x';
    el.style.color = 'rgb(255, 255, 255)';
    document.body.append(el);
    getComputedStyle(el).color;
    el.style.color = 'rgb(29, 39, 51)';
    const result = { color: getComputedStyle(el).color, transitions: el.getAnimations().length };
    el.remove();
    return result;
  });
  expect(shown).toEqual({ color: 'rgb(29, 39, 51)', transitions: 0 });
});

test('with reduced motion an element that fades in is shown at once', async ({ page }) => {
  await page.goto('/o/acme/f/goal/board');
  await expect(page.getByRole('heading', { level: 1, name: 'Goal board' })).toBeVisible();

  // The fade the dialogs, sheets, menus and tooltips open with, started within one task.
  const opacity = await page.evaluate(() => {
    const el = document.createElement('span');
    el.textContent = 'x';
    el.className = 'animate-[fade-in_150ms_ease-out]';
    document.body.append(el);
    const value = getComputedStyle(el).opacity;
    el.remove();
    return value;
  });
  expect(opacity).toBe('1');
});
