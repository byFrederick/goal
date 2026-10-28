// The Webhooks page (docs/design/screens/intake/webhooks.html) against the mock: software's two
// endpoints (cf-deploys, refused and created deliveries; uptime, which feeds nothing), the request
// dialog, New webhook and Rotate, each showing its secret once, and the Send a test panel. Not run in
// the build box, which has no browser; run with `pnpm --dir apps/web run e2e`.
import { expect, type Page, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll, isPhone, recordToasts } from './support';

const page_ = '/o/acme/f/software/intake/webhooks';

test.beforeEach(async ({ page, request }) => {
  await request.post('/__mock/reset');
  await recordToasts(page);
});

async function fits(page: Page) {
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
}

test.describe('the Webhooks page', () => {
  test('lists each endpoint with its address, its week and its newest deliveries', async ({ page }) => {
    await page.goto(page_);

    const cf = page.getByRole('region', { name: 'cf-deploys' });
    await expect(cf.getByText('2 refused this week')).toBeVisible();
    await expect(cf.getByText('7 delivered, 2 refused')).toBeVisible();
    await expect(cf.getByText('Refused: signature did not match')).toBeVisible();
    await expect(cf.getByText('Accepted, no trigger matched').first()).toBeVisible();
    await expect(cf.getByRole('link', { name: '#131' })).toBeVisible();

    const uptime = page.getByRole('region', { name: 'uptime' });
    await expect(uptime.getByText('Nothing. 3 deliveries were kept but created no items.')).toBeVisible();
    await expect(uptime.getByRole('link', { name: 'Create a trigger' })).toBeVisible();

    await fits(page);
  });

  test('opens a delivery request with its headers and body, and closes it', async ({ page }) => {
    await page.goto(page_);

    const cf = page.getByRole('region', { name: 'cf-deploys' });
    await cf
      .getByRole('button', { name: /View request/ })
      .first()
      .click();

    const dialog = page.getByRole('dialog', { name: 'Request' });
    await expect(dialog.getByText('user-agent')).toBeVisible();
    await expect(dialog.getByText(/"service":"goal-api"/)).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(dialog).toBeHidden();
  });

  test('shows a new webhook’s secret once, and never again after Done', async ({ page }) => {
    await page.goto(page_);

    await page.getByRole('button', { name: 'New webhook' }).first().click();
    await page.getByRole('textbox', { name: 'Name' }).fill('github-ci');
    await page.getByRole('radio', { name: 'GitHub' }).click();
    await page.getByRole('button', { name: 'Create webhook' }).click();

    const shown = page.getByRole('dialog', { name: 'Webhook created' });
    await expect(shown.locator('code')).toHaveText(/^[0-9a-f]{64}$/);
    await shown.getByRole('button', { name: 'Done' }).click();

    await expect(page.getByRole('dialog')).toHaveCount(0);
    await expect(page.locator('code', { hasText: /^[0-9a-f]{64}$/ })).toHaveCount(0);
    await expect(page.getByRole('region', { name: 'github-ci' })).toBeVisible();
  });

  test('rotates a secret after a confirmation and shows the new one once', async ({ page }) => {
    await page.goto(page_);

    const cf = page.getByRole('region', { name: 'cf-deploys' });
    await cf.getByRole('button', { name: 'Rotate' }).click();
    await page
      .getByRole('dialog', { name: /Rotate the signing secret/ })
      .getByRole('button', { name: 'Rotate secret' })
      .click();

    const shown = page.getByRole('dialog', { name: 'Secret rotated' });
    await expect(shown.locator('code')).toHaveText(/^[0-9a-f]{64}$/);
    await shown.getByRole('button', { name: 'Done' }).click();
    await expect(page.locator('code', { hasText: /^[0-9a-f]{64}$/ })).toHaveCount(0);
  });

  test('keeps Send a sample delivery unavailable, and fits the phone and tablet widths', async ({ page }) => {
    await page.goto(page_);

    const panel = page.getByRole('complementary', { name: 'Send a test' });
    await expect(panel.getByRole('button', { name: 'Send a sample delivery' })).toHaveAttribute(
      'aria-disabled',
      'true',
    );
    await expect(panel.getByText('Fields reach the agent as untrusted data, never as instructions.')).toBeVisible();

    await fits(page);
    if (isPhone(page)) await expect(page.getByRole('heading', { name: 'Webhooks', level: 1 })).toBeVisible();
  });
});
