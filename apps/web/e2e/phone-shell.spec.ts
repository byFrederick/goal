import { expect, test } from '@playwright/test';
import { expectNoHorizontalScroll, orgNav } from './support';

// The phone shell (below 768 px): the Goal mark opens the drawer, the bottom tab bar follows the factory.
test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

test.describe('the phone shell at 375 px', () => {
  test.use({ viewport: { width: 375, height: 812 } });

  test('the Goal mark opens the drawer and returns focus to it on Escape', async ({ page }) => {
    await page.goto('/o/acme/f/goal');
    const mark = page.getByRole('button', { name: 'Open menu' });
    await mark.click();
    const drawer = page.getByRole('dialog', { name: 'Acme' });
    await expect(drawer).toBeVisible();
    await expect(drawer.getByRole('link', { name: 'Knowledge' })).toBeVisible();

    // Focus stays inside the drawer while it is open.
    await page.keyboard.press('Tab');
    await expect(drawer.locator(':focus')).toHaveCount(1);

    await page.keyboard.press('Escape');
    await expect(drawer).toBeHidden();
    await expect(mark).toBeFocused();
  });

  test('Needs you links to its page from the tab bar', async ({ page }) => {
    await page.goto('/o/acme/f/goal');
    const needs = page.getByRole('navigation', { name: 'Tabs' }).getByRole('link', { name: /^Needs you/ });
    await expect(needs).toHaveAttribute('href', '/o/acme/needs-you');
    await needs.click();
    await expect(page).toHaveURL(/\/o\/acme\/needs-you$/);
  });

  test('Floor, Board and Items follow the last factory visited outside a factory', async ({ page }) => {
    await page.goto('/o/acme/f/sre/board');
    await page.goto('/o/acme');
    const tabs = page.getByRole('navigation', { name: 'Tabs' });
    await expect(tabs.getByRole('link', { name: 'Board' })).toHaveAttribute('href', '/o/acme/f/sre/board');
  });

  test('the page does not scroll sideways', async ({ page }) => {
    await page.goto('/o/acme/f/goal');
    await expectNoHorizontalScroll(page);
    await page.getByRole('button', { name: 'Open menu' }).click();
    await expect(page.getByRole('dialog', { name: 'Acme' })).toBeVisible();
    await expectNoHorizontalScroll(page);
  });

  test('the drawer lists the organization pages and the factories', async ({ page }) => {
    await page.goto('/o/acme');
    await page.getByRole('button', { name: 'Open menu' }).click();
    await expect(orgNav(page).filter({ visible: true }).getByRole('link', { name: 'Home' })).toBeVisible();
  });
});

test.describe('the phone shell at 390 px', () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test('nothing scrolls sideways on a factory page', async ({ page }) => {
    await page.goto('/o/acme/f/goal/board');
    await expectNoHorizontalScroll(page);
  });
});

test.describe('the phone shell is gone at 768 px', () => {
  test.use({ viewport: { width: 768, height: 1024 } });

  test('no tab bar and no Goal mark button; the sidebar shows', async ({ page }) => {
    await page.goto('/o/acme/f/goal');
    await expect(page.getByRole('navigation', { name: 'Tabs' })).toBeHidden();
    await expect(page.getByRole('button', { name: 'Open menu' })).toBeHidden();
    await expect(orgNav(page).filter({ visible: true })).toBeVisible();
  });
});
