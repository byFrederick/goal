// Loads that outlive the page that started them: a fetch whose only observer unmounts is
// cancelled, and the next page's loader that shares it must load again, not fail.
import { expect, test } from '@playwright/test';
import { isPhone, orgMarkOf } from './support';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

test('a page whose data the page left was still loading loads it again instead of failing', async ({
  page,
  request,
}) => {
  // The Line section reads the organization's settings (slot limits); its first read is slow.
  await request.post('/__mock/fail', { data: { path: '/settings', method: 'GET', delay_ms: 3000 } });
  await page.goto('/o/acme/f/goal/definition/line');
  await expect(page.getByRole('heading', { level: 2, name: 'Line', exact: true })).toBeVisible();

  // Organization settings shares that read; leaving the Line section cancels it.
  await orgMarkOf(page).click();
  const settings = isPhone(page)
    ? page.getByRole('dialog', { name: 'Switch organization' }).getByRole('link', { name: 'Organization settings' })
    : page.getByRole('menuitem', { name: 'Organization settings' });
  await settings.click();

  await expect(page.getByRole('heading', { level: 1, name: 'Organization settings' })).toBeVisible();
  await expect(page.getByLabel('Name')).toHaveValue('Acme');
  await expect(page.getByText('goald is not answering')).toHaveCount(0);
});
