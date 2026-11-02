// Models and policies (organization/models.html) against the mock: the screen opens as pictured, a model
// switch and a denied command change the form, and Save changes sends the policy and shows it saved.
// Not run in the box (no browser here): see the pull request.
import { expect, type Page, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll } from './support';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

const heading = (page: Page) => page.getByRole('heading', { level: 1, name: 'Models and policies' });

test('the screen shows the acme policy, and a model switch, a denied command and a save reach the mock', async ({
  page,
}) => {
  await page.goto('/o/acme/models');
  await expect(heading(page)).toBeVisible();
  await expect(page).toHaveTitle('Models and policies · Acme · Goal');
  await expect(page.getByRole('switch', { name: 'Allow Claude Sonnet 5.5' })).toHaveAttribute('aria-checked', 'true');

  // Turn Sonnet off, deny a command, and save.
  await page.getByRole('switch', { name: 'Allow Claude Sonnet 5.5' }).click();
  await page.getByRole('button', { name: '+ Add a command' }).click();
  await page.getByRole('textbox', { name: 'Command' }).fill('gh release');
  await page.getByRole('button', { name: 'Add', exact: true }).click();
  await expect(page.getByText('gh release', { exact: true })).toBeVisible();

  const save = page.getByRole('button', { name: 'Save changes' });
  await expect(save).toBeEnabled();
  const put = page.waitForResponse((r) => r.request().method() === 'PUT' && r.url().includes('/policy'));
  await save.click();
  expect((await put).status()).toBe(200);
  await expect(save).toBeDisabled();
  await expect(page.getByRole('switch', { name: 'Allow Claude Sonnet 5.5' })).toHaveAttribute('aria-checked', 'false');

  // The routers row and another harness are not built yet.
  await expect(page.getByRole('button', { name: 'Add a router' })).toHaveAttribute('aria-disabled', 'true');
  await expect(page.getByRole('radio', { name: 'Another harness' })).toHaveAttribute('aria-disabled', 'true');
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('the Harness title is not clipped at 1024 px', async ({ page }) => {
  await page.setViewportSize({ width: 1024, height: 900 });
  await page.goto('/o/acme/models');
  const title = page.getByRole('heading', { name: 'Harness', exact: true });
  await expect(title).toBeVisible();
  const clipped = await title.evaluate((el) => el.scrollWidth > el.clientWidth);
  expect(clipped).toBe(false);
  await expectNoHorizontalScroll(page);
});

for (const width of [375, 768, 1440]) {
  test(`the screen fits at ${width} px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/o/acme/models');
    await expect(heading(page)).toBeVisible();
    await expectNoHorizontalScroll(page);
  });
}
