// Factory settings (canvas build/factory-settings) against the mock (Code with its repositories, Runtime with its environments line, the model key and the environments, Notifications with the Slack secret): at 1440, 1280, 1024, 768
// and 390 px, in light and dark, nothing goes past the viewport edge, no text is clipped, and a phone's
// tap targets are 44 px. These specs are not run in the box (no browser); CI runs them.
import { expect, test } from '@playwright/test';
import { expectAccessible, expectNoClippedElements, expectNoHorizontalScroll } from './support';
import { expectNoOverlap, expectTarget44 } from './support-boxes';

const sizes = [
  { width: 1440, height: 1000 },
  { width: 1280, height: 900 },
  { width: 1024, height: 900 },
  { width: 768, height: 1024 },
  { width: 390, height: 844 },
];

const path = '/o/acme/f/goal/settings';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

for (const size of sizes) {
  for (const scheme of ['light', 'dark'] as const) {
    test(`fits ${size.width} px in ${scheme}`, async ({ page }, testInfo) => {
      test.skip(testInfo.project.name !== 'desktop-1440', 'one project runs the whole matrix');
      await page.setViewportSize(size);
      await page.emulateMedia({ colorScheme: scheme });
      await page.goto(path);
      await expect(page.getByRole('region', { name: 'Runtime' })).toBeVisible();
      await expect(page.getByRole('region', { name: 'Code' })).toBeVisible();
      await expect(page.getByRole('region', { name: 'Notifications' }).getByText('slack-bot')).toBeVisible();

      await expectNoHorizontalScroll(page);
      await expectNoClippedElements(page);
      await testInfo.attach(`factory-settings-${size.width}-${scheme}`, {
        body: await page.screenshot(),
        contentType: 'image/png',
      });
    });
  }
}

test('keeps the 44 px tap targets on a phone', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop-1440', 'one project runs the whole matrix');
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(path);
  await expect(page.getByRole('region', { name: 'Runtime' })).toBeVisible();
  // A radio's tap target is its whole label, and a switch's is its 44 px ::before hit area (ui/switch.tsx),
  // which getBoundingClientRect does not measure; the Switch is drawn 32 × 18 by design.
  const targets = page
    .getByRole('main')
    .locator(
      'a[href]:not(p a, dd a), button:not([role=switch]), input:not([type=hidden], [type=radio]), label:has(input[type=radio]), [role=combobox]',
    );
  const boxes = await targets.evaluateAll((els) =>
    els.map((el) => {
      const { width, height } = el.getBoundingClientRect();
      return { name: (el.getAttribute('aria-label') ?? el.textContent ?? '').trim().slice(0, 30), width, height };
    }),
  );
  for (const box of boxes.filter((b) => b.width > 8 && b.height > 8)) {
    expect(box.height, `${box.name} is ${box.height}px tall`).toBeGreaterThanOrEqual(44);
  }
  await expectAccessible(page);
});

test('the Code section’s ⓘ buttons are 44 px on a phone', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop-1440', 'one project runs the whole matrix');
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(path);
  const code = page.getByRole('region', { name: 'Code' });
  await expect(code).toBeVisible();
  for (const tip of ['About Agents that write', 'About Secrets and webhooks']) {
    await expectTarget44(code.getByRole('button', { name: tip }), tip);
  }
});

test('the environments Edit link is 44×44 and no Jonah ⓘ lies over the field under it at 390 px', async ({
  page,
}, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop-1440', 'one project runs the whole matrix');
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(path);
  await expect(page.getByRole('region', { name: 'Runtime' })).toBeVisible();
  await expectTarget44(
    page.getByRole('region', { name: 'Runtime' }).getByRole('link', { name: 'Edit', exact: true }),
    'Edit',
  );
  const jonah = page.getByRole('region', { name: 'Jonah' });
  const tips = [
    ['About Jonah’s model', 'Model'],
    ['About Jonah’s budget', 'Budget, % of the line’s spend'],
    ['About proposing', 'Proposes after, items'],
  ] as const;
  for (const [tip, field] of tips) {
    const button = jonah.getByRole('button', { name: tip });
    await expectTarget44(button, tip);
    await expectNoOverlap(
      button,
      jonah.getByRole(field === 'Model' ? 'combobox' : 'spinbutton', { name: field }),
      `${tip} and ${field}`,
    );
  }
});
