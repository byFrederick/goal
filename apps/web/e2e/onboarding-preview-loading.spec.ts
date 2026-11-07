// The onboarding preview while a chosen factory loads, at 375 and 1440 px: the panel draws placeholders and
// never asks for a template again. Not run in the box (no browser); CI runs it.
import { expect, test } from '@playwright/test';

const widths = [375, 1440];

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

for (const width of widths) {
  test(`reloading Line on a published factory never asks for a template at ${width} px`, async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'desktop-1440', 'one project runs both widths');
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/o/goal/get-started/factory');
    await page.getByRole('button', { name: /^Use / }).click();
    await expect(page).toHaveURL(/\/o\/goal\/get-started\/code\?.*factory=/);
    const factory = new URL(page.url()).searchParams.get('factory');

    // A retrying assertion would pass once the sentence goes away, so watch the DOM for it from the first paint.
    await page.addInitScript(() => {
      const w = window as unknown as { __askedForTemplate?: boolean };
      const look = () => {
        if (document.body?.textContent?.includes('The line appears when you pick a template'))
          w.__askedForTemplate = true;
      };
      new MutationObserver(look).observe(document, { childList: true, subtree: true, characterData: true });
    });
    await page.setExtraHTTPHeaders({ 'X-Mock-Latency-Ms': '1500' });
    await page.goto(`/o/goal/get-started/line?factory=${factory}`);
    await expect(page.getByRole('heading', { level: 1, name: 'Shape the line' })).toBeVisible();
    expect(
      await page.evaluate(() => (window as unknown as { __askedForTemplate?: boolean }).__askedForTemplate),
    ).toBeFalsy();
  });
}
