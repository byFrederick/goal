// Overlay motion (MOTION.md, the catalogue rows for menus and dialogs). A menu and a dialog open on a CSS
// transition and stay mounted while they close; closing one while it is still opening continues from the
// opacity it has, not from 0. Under reduced motion the fade stays and the scale goes. Runs on the members
// page as an owner, where the organization menu and the Invite dialog open.
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
 * Stretches the overlay durations, so the opening transition outlasts the round trips between the test and
 * the page and the test closes the panel while it opens, however loaded the machine is. The transitions
 * and the rule under test are the same; only their length changes.
 */
async function slowOverlays(page: Page) {
  await page.addStyleTag({ content: ':root { --duration-quick: 3s; --duration-fast: 3s; }' });
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

  // The sampler is armed before Escape and reads the opacity the moment the panel's data-state turns to
  // "closed", then once a frame. The first read is where the close starts, so it does not depend on how
  // long the key press takes to reach the page (a reversed transition is shortened and can end before a
  // second round trip).
  await page.evaluate((sel) => {
    const el = document.querySelector<HTMLElement>(sel);
    if (!el) throw new Error(`${sel} is not on the page`);
    const closed = new Promise<void>((resolve) => {
      const watch = new MutationObserver(() => {
        if (el.getAttribute('data-state') !== 'closed') return;
        watch.disconnect();
        resolve();
      });
      watch.observe(el, { attributes: true, attributeFilter: ['data-state'] });
    });
    (window as unknown as { sampled: Promise<number[]> }).sampled = closed.then(async () => {
      const values: number[] = [];
      for (let frame = 0; frame < 60; frame++) {
        const panel = document.querySelector<HTMLElement>(sel);
        if (!panel) break;
        values.push(Number(getComputedStyle(panel).opacity));
        await new Promise((resolve) => requestAnimationFrame(resolve));
      }
      return values;
    });
  }, selector);
  await page.keyboard.press('Escape');
  const samples = await page.evaluate(() => (window as unknown as { sampled: Promise<number[]> }).sampled);

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
  await slowOverlays(page);
  // The organization menu opens at every width. Below 768 px the sidebar is the drawer the Goal mark opens;
  // a member row's More menu is only there beside a table, which the narrow layouts replace with cards.
  if ((page.viewportSize()?.width ?? 0) < 768) await page.getByRole('button', { name: 'Open menu' }).click();
  await page.getByRole('button', { name: /organization, switch organization$/ }).click();

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
  await slowOverlays(page);
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
