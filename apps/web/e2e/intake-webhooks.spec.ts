// Webhooks moved from the factory to the organization (ADR 0040 decision 12): the factory's old page goes to
// the organization's Webhooks page, which the sidebar links. Not run in the build box, which has no browser;
// run with `pnpm --dir apps/web run e2e`. The mock has no organization webhooks yet, so the page's own
// lists, openings and delivery log are covered by Vitest (src/features/webhooks).
import { expect, test } from '@playwright/test';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

test('the factory’s old Webhooks address goes to the organization’s page', async ({ page }) => {
  await page.goto('/o/acme/f/software/intake/webhooks');

  await expect(page).toHaveURL(/\/o\/acme\/webhooks$/);
  await expect(page.getByRole('heading', { name: 'Webhooks', level: 1 })).toBeVisible();
});
