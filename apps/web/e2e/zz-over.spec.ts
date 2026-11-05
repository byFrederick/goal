import { test } from '@playwright/test';
test('over', async ({ page }) => {
  const [url, w, h] = (process.env.OVER ?? '').split(',');
  await page.setViewportSize({ width: Number(w), height: Number(h ?? 900) });
  await page.goto(url);
  await page.waitForTimeout(1200);
  console.log(await page.evaluate(() => {
    const vw = document.documentElement.clientWidth;
    const out: string[] = [];
    for (const e of Array.from(document.querySelectorAll('body *'))) {
      const r = e.getBoundingClientRect();
      if (r.width > 0 && r.right > vw + 1) out.push(`${e.tagName}.${String(e.className).slice(0, 80)} right=${Math.round(r.right)} w=${Math.round(r.width)} :: ${(e.textContent ?? '').slice(0, 30)}`);
    }
    return `scrollW=${document.documentElement.scrollWidth} vw=${vw}\n` + out.slice(0, 12).join('\n');
  }));
});
