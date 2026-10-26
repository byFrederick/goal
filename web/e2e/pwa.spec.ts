import { expect, test } from '@playwright/test';

// The home-screen app (docs/engineering/frontend.md, PWA): the worker registers at the root, the
// manifest loads, and Chrome finds nothing that stops the install. Runs against the preview build.
test.use({ serviceWorkers: 'allow' });

test('registers the service worker at the root scope', async ({ page }) => {
  await page.goto('/');
  const scope = await page.evaluate(async () => {
    const registration = await navigator.serviceWorker.ready;
    return { scope: registration.scope, script: registration.active?.scriptURL ?? '' };
  });
  expect(new URL(scope.scope).pathname).toBe('/');
  expect(new URL(scope.script).pathname).toBe('/sw.js');
});

test('loads and parses the linked manifest', async ({ page }) => {
  await page.goto('/');
  const href = await page.locator('link[rel="manifest"]').getAttribute('href');
  expect(href).toBeTruthy();
  const response = await page.request.get(new URL(href ?? '', page.url()).toString());
  expect(response.ok()).toBe(true);
  const manifest = (await response.json()) as { name: string; start_url: string; icons: unknown[] };
  expect(manifest.name).toBe('Goal');
  expect(manifest.start_url).toBe('/');
  expect(manifest.icons.length).toBeGreaterThan(0);
});

test('Chrome reports the app installable', async ({ page }) => {
  await page.goto('/');
  await page.evaluate(() => navigator.serviceWorker.ready);
  const cdp = await page.context().newCDPSession(page);
  await cdp.send('Page.enable');
  const { installabilityErrors } = await cdp.send('Page.getInstallabilityErrors');
  expect(installabilityErrors).toEqual([]);
});
