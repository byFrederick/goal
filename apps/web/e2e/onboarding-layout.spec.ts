// Onboarding step 2, Connect your code (canvas get-started/repository) against the mock (a repository URL, a write-only token, the host's answer): at 1440, 1280, 1024, 768
// and 390 px, in light and dark, nothing goes past the viewport edge, no text is clipped, and a phone's
// tap targets are 44 px. These specs are not run in the box (no browser); CI runs them.
import { expect, type Page, test } from '@playwright/test';
import { expectAccessible, expectNoClippedElements, expectNoHorizontalScroll } from './support';

const sizes = [
  { width: 1440, height: 1000 },
  { width: 1280, height: 900 },
  { width: 1024, height: 900 },
  { width: 768, height: 1024 },
  { width: 390, height: 844 },
];

const path = '/o/goal/get-started/factory';

/** Makes the factory on step 2, which opens the Code step. */
async function openCodeStep(page: Page) {
  await page.goto(path);
  await page.getByRole('button', { name: /^Use / }).click();
  await expect(page).toHaveURL(/\/o\/goal\/get-started\/code\?.*factory=/);
}

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

for (const size of sizes) {
  for (const scheme of ['light', 'dark'] as const) {
    test(`fits ${size.width} px in ${scheme}`, async ({ page }, testInfo) => {
      test.skip(testInfo.project.name !== 'desktop-1440', 'one project runs the whole matrix');
      await page.setViewportSize(size);
      await page.emulateMedia({ colorScheme: scheme });
      await openCodeStep(page);
      await expect(page.getByRole('heading', { level: 1, name: 'Connect your code' })).toBeVisible();

      await expectNoHorizontalScroll(page);
      await expectNoClippedElements(page);
      await testInfo.attach(`onboarding-${size.width}-${scheme}`, {
        body: await page.screenshot(),
        contentType: 'image/png',
      });
    });
  }
}

test('keeps the 44 px tap targets on a phone', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop-1440', 'one project runs the whole matrix');
  await page.setViewportSize({ width: 390, height: 844 });
  await openCodeStep(page);
  await expect(page.getByRole('heading', { level: 1, name: 'Connect your code' })).toBeVisible();
  const targets = page
    .getByRole('main')
    .locator('a[href]:not(p a, dd a), button, input:not([type=hidden]), [role=combobox]');
  const boxes = await targets.evaluateAll((els) =>
    els.map((el) => {
      // The hit area, not the drawn box: an input inside a label is hit through the label, and an
      // absolutely positioned ::before with negative insets grows its element's box.
      const label = el instanceof HTMLInputElement ? el.closest('label') : null;
      const rect = (label ?? el).getBoundingClientRect();
      let { width, height } = rect;
      const before = getComputedStyle(el, '::before');
      if (!label && before.position === 'absolute' && before.content !== 'none') {
        const grow = (v: string) => Math.max(0, -(Number.parseFloat(v) || 0));
        width += grow(before.left) + grow(before.right);
        height += grow(before.top) + grow(before.bottom);
      }
      return { name: (el.getAttribute('aria-label') ?? el.textContent ?? '').trim().slice(0, 30), width, height };
    }),
  );
  for (const box of boxes.filter((b) => b.width > 8 && b.height > 8)) {
    expect(box.height, `${box.name} is ${box.height}px tall`).toBeGreaterThanOrEqual(44);
  }
  await expectAccessible(page);
});

test('gives the purpose buttons and the Budget sliders a 44 px target at 390 px', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop-1440', 'one project runs the whole matrix');
  await page.setViewportSize({ width: 390, height: 844 });
  await page.request.post('/__mock/reset', {
    data: { orgs: 'none', auth: { mode: 'session', signed_in: 'fresh', email: 'f.cruz@example.com' } },
  });
  await page.goto('/get-started');
  for (const name of ['Build software', 'Run operations', 'Answer support', 'Something else']) {
    const box = await page.getByRole('button', { name }).boundingBox();
    expect(box?.height ?? 0, name).toBeGreaterThanOrEqual(44);
  }

  await page.request.post('/__mock/reset');
  await openCodeStep(page);
  const factory = new URL(page.url()).searchParams.get('factory');
  await page.goto(`/o/goal/get-started/budget?factory=${factory}`);
  await expect(page.locator('#budget-day')).toBeVisible();
  for (const id of ['budget-day', 'budget-week', 'budget-item']) {
    const box = await page.locator(`#${id}`).boundingBox();
    expect(box?.height ?? 0, id).toBeGreaterThanOrEqual(44);
  }
});

test('keeps the current step’s label inside the stepper at 390 px on every step', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop-1440', 'one project runs the whole matrix');
  await page.setViewportSize({ width: 390, height: 844 });
  await openCodeStep(page);
  const search = new URL(page.url()).search;
  for (const step of ['code', 'line', 'budget', 'intake', 'people', 'first-item']) {
    await page.goto(`/o/goal/get-started/${step}${search}`);
    const item = page.locator('li[aria-current=step]');
    await expect(item).toHaveCount(1);
    await expect
      .poll(
        async () => {
          const box = await item.boundingBox();
          const view = await page.getByRole('region', { name: 'Setup steps' }).boundingBox();
          if (!box || !view) return false;
          return box.x >= view.x - 0.5 && box.x + box.width <= view.x + view.width + 0.5;
        },
        { message: `the label of ${step} is inside the stepper` },
      )
      .toBe(true);
  }
});
