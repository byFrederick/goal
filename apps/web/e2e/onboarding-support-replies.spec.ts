// Onboarding with the Support replies template and No code, at 375, 768 and 1440 px, in light and dark: what the
// 2026-11-07 production walk fixed. The factory's title (not its slug) in the name field and the preview, a Code
// card for No code, the starter trigger in words, a model key card with a 44 px Show button, an organization
// address that stays inside its card, and axe on each step. One project runs the whole matrix. Not run in the
// box (no browser); CI runs it against the mock.
import { expect, type Page, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll } from './support';

const sizes = [
  { width: 375, height: 812 },
  { width: 768, height: 1024 },
  { width: 1440, height: 1000 },
];

const preview = (page: Page) => page.getByRole('complementary', { name: 'Preview of your factory' });

/** A new organization, Acme, on the Factory step. */
async function newOrganization(page: Page) {
  const reset = await page.request.post('/__mock/reset', {
    data: { orgs: 'none', auth: { mode: 'session', signed_in: 'fresh', email: 'f.cruz@example.com' } },
  });
  expect(reset.ok()).toBeTruthy();
  await page.goto('/get-started');
  await page.getByLabel('Organization name').fill('Acme');
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page).toHaveURL(/\/o\/acme\/get-started\/factory$/);
}

/** The organization card's address stays inside the card. */
async function expectAddressInsideCard(page: Page) {
  const address = preview(page).getByText(/\/o\/acme$/);
  await expect(address).toBeVisible();
  const fits = await address.evaluate((el) => {
    const card = el.parentElement as HTMLElement;
    return {
      right: el.getBoundingClientRect().right,
      cardRight: card.getBoundingClientRect().right,
      scrollWidth: card.scrollWidth,
      clientWidth: card.clientWidth,
    };
  });
  expect(fits.right).toBeLessThanOrEqual(fits.cardRight + 0.5);
  expect(fits.scrollWidth).toBeLessThanOrEqual(fits.clientWidth);
}

for (const size of sizes) {
  for (const scheme of ['light', 'dark'] as const) {
    test(`walks Support replies with No code at ${size.width} px in ${scheme}`, async ({ page }, testInfo) => {
      test.skip(testInfo.project.name !== 'desktop-1440', 'one project runs the whole matrix');
      await page.setViewportSize(size);
      await page.emulateMedia({ colorScheme: scheme });
      await newOrganization(page);
      const small = size.width <= 768;

      // Factory: the name is the title, and so is the preview's factory card.
      await page.getByRole('radio', { name: /Support replies/ }).check();
      await expect(page.getByLabel('Factory name')).toHaveValue('Support replies');
      const card = preview(page).getByRole('region', { name: 'Your factory' });
      await expect(card).toContainText('Support replies');
      await expect(card).not.toContainText('support-replies');
      await expectNoHorizontalScroll(page);
      await expectAccessible(page);
      if (small) await expectAddressInsideCard(page);
      await page.getByRole('button', { name: /^Use / }).click();

      // Code: No code makes a Code card, not the dashed placeholder.
      await expect(page).toHaveURL(/\/o\/acme\/get-started\/code\?.*factory=/);
      await expect(page.getByRole('heading', { level: 1, name: 'Connect your code' })).toBeVisible();
      await expectNoHorizontalScroll(page);
      await expectAccessible(page);
      await page.getByRole('radio', { name: /^No code/ }).check();
      await page.getByRole('button', { name: 'Continue' }).click();
      const codeCard = () => preview(page).getByText('Code', { exact: true }).locator('..');
      await expect(page).toHaveURL(/\/o\/acme\/get-started\/line\?.*factory=/);

      // Line: the model key card, once, with a Show button of 44 px on a phone.
      await expect(page.getByRole('heading', { level: 1, name: 'Shape the line' })).toBeVisible();
      await expect(codeCard()).toContainText('No code');
      await expect(page.getByText('Model key', { exact: true })).toHaveCount(1);
      const show = page.getByRole('button', { name: 'Show the value' });
      await expect(show).toBeVisible();
      if (size.width === 375) {
        const box = await show.boundingBox();
        expect(box?.height ?? 0).toBeGreaterThanOrEqual(44);
        expect(box?.width ?? 0).toBeGreaterThanOrEqual(44);
      }
      if (small) await expectAddressInsideCard(page);
      await expectNoHorizontalScroll(page);
      await expectAccessible(page);
      await page
        .locator('input')
        .and(page.getByLabel('Model key', { exact: true }))
        .fill('test-token');
      await page.getByRole('button', { name: 'Save the key' }).click();
      await expect(page.getByText('Saved as the secret model-key, open to this factory.')).toBeVisible();
      await page.getByRole('button', { name: 'Continue' }).click();

      // Budget.
      await expect(page.getByRole('heading', { level: 1, name: 'Set budgets and targets' })).toBeVisible();
      await expect(codeCard()).toContainText('No code');
      await expectNoHorizontalScroll(page);
      await expectAccessible(page);
      await page.getByRole('button', { name: 'Continue' }).click();

      // Intake: the trigger in words, the station named Inbox.
      await expect(page).toHaveURL(/\/o\/acme\/get-started\/intake\?.*factory=/);
      await expect(page.getByRole('heading', { level: 1, name: 'Choose where work comes from' })).toBeVisible();
      await expect(page.getByText('Weekdays at 07:00 UTC')).toBeVisible();
      await expect(page.getByText('Inbox').first()).toBeVisible();
      await expect(codeCard()).toContainText('No code');
      await expectNoHorizontalScroll(page);
      await expectAccessible(page);
    });
  }
}
