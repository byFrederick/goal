// The built app under the Content-Security-Policy goald serves it with (apps/web/csp.txt; vite preview
// sends the same): no violations on the work screens or the YAML view, whose Monaco loads its
// workers, fonts and styles from the app's own origin.
import { expect, test } from '@playwright/test';
import { openYaml } from './editor';
import { isPhone } from './support';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

test('the app runs under its Content-Security-Policy, Monaco and its workers too', async ({ page }) => {
  const violations: string[] = [];
  page.on('console', (m) => {
    if (/Content.Security.Policy/i.test(m.text())) violations.push(m.text());
  });
  await page.addInitScript(() => {
    document.addEventListener('securitypolicyviolation', (e) =>
      console.error(`Content Security Policy violation: ${e.effectiveDirective} ${e.blockedURI}`),
    );
  });
  const workers: string[] = [];
  page.on('worker', (w) => workers.push(w.url()));

  const response = await page.goto('/o/acme/f/goal');
  expect(response?.headers()['content-security-policy']).toContain("frame-ancestors 'none'");
  expect(response?.headers()['content-security-policy']).toContain("object-src 'none'");
  expect(response?.headers()['x-frame-options']).toBe('DENY');
  expect(response?.headers()['cross-origin-opener-policy']).toBe('same-origin');
  await expect(page.getByRole('heading', { level: 1, name: 'Goal floor' })).toBeVisible();

  await page.goto('/o/acme/f/goal/board');
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
  await page.goto('/o/acme/f/goal/items/12');
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

  await openYaml(page);
  if (!isPhone(page)) await expect.poll(() => workers.length).toBeGreaterThan(0);
  expect(violations).toEqual([]);
});
