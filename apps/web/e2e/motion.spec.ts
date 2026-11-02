// Overlay motion (MOTION.md, the catalogue rows for menus and dialogs). A menu and a dialog open on a CSS
// transition and stay mounted while they close; closing one while it is still opening continues from the
// opacity it has, not from 0. Under reduced motion the fade stays and the scale goes. Runs on the members
// page as an owner, where a row's "More" menu and the Invite dialog open.
import { type APIRequestContext, expect, type Locator, type Page, test } from '@playwright/test';

async function signedInAsOwner(request: APIRequestContext) {
  const res = await request.post('/__mock/reset', {
    data: { auth: { mode: 'session', signed_in: 'fresh', email: 'f.cruz@example.com' } },
  });
  expect(res.ok()).toBe(true);
}

test.afterEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

/** The transitions the element runs in its current state: the property and its duration in seconds. */
function transitionsOf(locator: Locator) {
  return locator.evaluate((el) => {
    const style = getComputedStyle(el);
    const properties = style.transitionProperty.split(',').map((p) => p.trim());
    const durations = style.transitionDuration.split(',').map((d) => Number.parseFloat(d));
    return properties.map((property, i) => ({ property, seconds: durations[i % durations.length] ?? 0 }));
  });
}

/**
 * Closes the panel while its opening transition runs. The opacity transition is paused a fifth of the way
 * in and Escape closes the panel. The opacity sampled from then on starts where the open stopped, falls
 * without jumping to 0, and the panel leaves the page only once its close has run.
 */
async function closeWhileOpening(page: Page, selector: string) {
  await page.waitForFunction((sel) => {
    const el = document.querySelector<HTMLElement>(sel);
    return !!el && el.getAnimations().some((a) => a instanceof CSSTransition && a.transitionProperty === 'opacity');
  }, selector);

  const before = await page.evaluate((sel) => {
    const el = document.querySelector<HTMLElement>(sel);
    if (!el) throw new Error(`${sel} is not on the page`);
    for (const animation of el.getAnimations()) {
      animation.pause();
      animation.currentTime = Number(animation.effect?.getComputedTiming().duration) * 0.2;
    }
    return Number(getComputedStyle(el).opacity);
  }, selector);

  await page.keyboard.press('Escape');

  const samples = await page.evaluate(async (sel) => {
    const values: number[] = [];
    for (let frame = 0; frame < 60; frame++) {
      const el = document.querySelector<HTMLElement>(sel);
      if (!el) break;
      values.push(Number(getComputedStyle(el).opacity));
      await new Promise((resolve) => requestAnimationFrame(resolve));
    }
    return values;
  }, selector);

  expect(before).toBeGreaterThan(0);
  expect(before).toBeLessThan(1);
  const first = samples[0] ?? 0;
  expect(samples.length).toBeGreaterThan(1);
  expect(first).toBeGreaterThan(0);
  expect(Math.abs(first - before)).toBeLessThan(0.1);
  expect(samples.every((value, i) => i === 0 || value <= (samples[i - 1] ?? 0) + 1e-6)).toBe(true);
  await expect(page.locator(selector)).toHaveCount(0);
}

test('a menu opens on a transition from its trigger, and closing it while it opens continues from its opacity', async ({
  page,
}) => {
  await signedInAsOwner(page.request);
  await page.goto('/o/acme/members');
  await page
    .getByRole('button', { name: /^More for / })
    .first()
    .click();

  const menu = page.getByRole('menu');
  await expect(menu).toBeVisible();
  const moving = (await transitionsOf(menu)).filter(
    (t) => (t.property === 'opacity' || t.property === 'transform') && t.seconds > 0,
  );
  expect(moving.length).toBeGreaterThan(0);

  await closeWhileOpening(page, '[role="menu"]');
});

test('a dialog opens on a transition, and closing it while it opens continues from its opacity', async ({ page }) => {
  await signedInAsOwner(page.request);
  await page.goto('/o/acme/members');
  await page.getByRole('button', { name: /^Invite/ }).click();

  const dialog = page.getByRole('dialog', { name: 'Invite people to Acme' });
  await expect(dialog).toBeVisible();
  const moving = (await transitionsOf(dialog)).filter(
    (t) => (t.property === 'opacity' || t.property === 'transform') && t.seconds > 0,
  );
  expect(moving.length).toBeGreaterThan(0);

  await closeWhileOpening(page, '[role="dialog"]');
});

test.describe('with reduced motion', () => {
  test.use({ reducedMotion: 'reduce' });

  test('a dialog keeps its fade and never scales', async ({ page }) => {
    await signedInAsOwner(page.request);
    await page.goto('/o/acme/members');
    await page.getByRole('button', { name: /^Invite/ }).click();

    const dialog = page.getByRole('dialog', { name: 'Invite people to Acme' });
    await expect(dialog).toBeVisible();

    const transforms = await dialog.evaluate(async (el) => {
      const seen: string[] = [];
      for (let frame = 0; frame < 15; frame++) {
        seen.push(getComputedStyle(el).transform);
        await new Promise((resolve) => requestAnimationFrame(resolve));
      }
      return seen;
    });
    expect(transforms.every((transform) => transform === 'none')).toBe(true);

    const opacity = (await transitionsOf(dialog)).find((t) => t.property === 'opacity');
    expect(opacity?.seconds ?? 0).toBeGreaterThan(0);
  });
});
