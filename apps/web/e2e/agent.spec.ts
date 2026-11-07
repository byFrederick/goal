// E12 and build/agent: an agent's statistics, why review sent its work back, and its tabs (?view=).
import { expect, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll, isPhone } from './support';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

test('the agent page shows the last 7 days, its station and why work was sent back', async ({ page }) => {
  await page.goto('/o/acme/f/sre/agents/investigator');
  await expect(page.getByRole('heading', { level: 1, name: 'investigator' })).toBeVisible();
  await expect(page).toHaveTitle(/^investigator · SRE · Acme · Goal$/);
  await expect(page.getByText('Investigate station')).toBeVisible();

  const measures = page.getByRole('region', { name: 'Last 7 days' });
  // NumberRoll renders each figure twice (MOTION.md, "A number changes"): an sr-only copy, then the aria-hidden digits.
  // Read the shown one, the last.
  await expect(measures.getByText('31', { exact: true }).last()).toBeVisible();
  await expect(measures.getByText('22%', { exact: true }).last()).toBeVisible();
  await expect(measures.getByText('$0.94').last()).toBeVisible();
  await expect(measures.getByText('24 min').last()).toBeVisible();

  const reasons = page.getByRole('region', { name: 'Why review sent work back' });
  await expect(reasons.getByRole('listitem')).toHaveCount(3);
  await expect(reasons.getByRole('listitem').first()).toContainText('No CloudWatch evidence for a latency claim');
  await expect(reasons.getByRole('listitem').first()).toContainText('4×');

  await page.getByRole('navigation', { name: 'Agent views' }).getByRole('link', { name: 'Tools and access' }).click();
  await expect(page.getByText('Blocked this week: 2 calls, both aws ssm put-parameter.')).toBeVisible();

  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('the agent page at 768 keeps the crumbs clear of the actions and does not scroll sideways', async ({ page }) => {
  await page.setViewportSize({ width: 768, height: 900 });
  await page.goto('/o/acme/f/sre/agents/investigator');
  await expect(page.getByRole('heading', { level: 1, name: 'investigator' })).toBeVisible();
  await expectNoHorizontalScroll(page);
  const crumbs = await page.getByRole('navigation', { name: 'Breadcrumb' }).boundingBox();
  const action = await page.getByRole('link', { name: 'Open prompt file' }).boundingBox();
  expect(crumbs).not.toBeNull();
  expect(action).not.toBeNull();
  // Beside the actions, or above them when they wrap to a row under the crumbs.
  const beside = (crumbs?.x ?? 0) + (crumbs?.width ?? 0) <= (action?.x ?? 0);
  const above = (crumbs?.y ?? 0) + (crumbs?.height ?? 0) <= (action?.y ?? 0);
  expect(beside || above).toBe(true);
});

test('Try another model opens its dialog from More actions at 375 px', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto('/o/acme/f/goal/agents/reviewer');
  const more = page.getByRole('button', { name: 'More actions' });
  await more.click();
  await page.getByRole('menuitem', { name: /Try another model/ }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  await expect(more).toBeFocused();
});

test('an agent without history says so, and an unknown agent is not found', async ({ page }) => {
  await page.goto('/o/acme/f/goal/agents/reviewer');
  await expect(page.getByRole('heading', { level: 1, name: 'reviewer' })).toBeVisible();
  await expect(page.getByText('Nothing was sent back in the last 7 days.')).toBeVisible();
  await page.getByRole('navigation', { name: 'Agent views' }).getByRole('link', { name: 'Tools and access' }).click();
  await expect(page.getByText('Blocked this week: no calls.')).toBeVisible();

  await page.goto('/o/acme/f/goal/agents/nobody');
  await expect(page.getByRole('heading', { level: 1, name: 'No such agent' })).toBeVisible();
});

test('the agent page reads as pictured: the prompt, settings and the Not available control', async ({ page }) => {
  await page.goto('/o/acme/f/goal/agents/frontend-engineer');
  await expect(page.getByRole('heading', { level: 1, name: 'frontend-engineer' })).toBeVisible();
  await expect(page.getByText('Build station')).toBeVisible();
  await expect(
    page.getByText('The React UI in apps/web and the TypeScript packages, following the design system exactly.'),
  ).toBeVisible();

  const reasons = page.getByRole('region', { name: 'Why review sent work back' });
  await expect(reasons.getByRole('listitem')).toHaveCount(4);
  await expect(page.getByText(/You build Goal's web app/)).toBeVisible();
  await expect(page.getByRole('region', { name: 'Settings' }).getByText('claude-haiku-5-5')).toBeVisible();

  // The Settings aside stacks under the main column below xl (1280 px) and sits beside it at xl and up.
  const viewport = page.viewportSize();
  const settings = await page.getByRole('region', { name: 'Settings' }).boundingBox();
  const measures = await page.getByRole('region', { name: 'Last 7 days' }).boundingBox();
  expect(viewport && settings && measures).toBeTruthy();
  if (viewport && settings && measures) {
    if (viewport.width >= 1280) {
      expect(settings.x).toBeGreaterThanOrEqual(measures.x + measures.width);
      expect(settings.y).toBeLessThan(measures.y + measures.height);
    } else {
      expect(settings.x).toBeLessThan(measures.x + measures.width / 2);
      expect(settings.y).toBeGreaterThanOrEqual(measures.y + measures.height);
    }
  }

  // Reset to default sits beside the prompt header and Edit prompt.
  const promptRegion = page.getByRole('region', { name: 'Prompt' });
  await expect(promptRegion.getByRole('button', { name: 'Reset to default' })).toBeVisible();
  await expect(promptRegion.getByRole('link', { name: 'Edit prompt' })).toBeVisible();

  // Ask Jonah for a fix is not built yet (ADR 0021 runner pending): it stays disabled and sends nothing.
  const askJonah = page.getByRole('button', { name: 'Ask Jonah for a fix' });
  await expect(askJonah).toHaveAttribute('aria-disabled', 'true');
  // The button is aria-disabled, which Playwright's click waits on forever: force the click, as a person's tap would.
  await askJonah.click({ force: true });
  await expect(page).not.toHaveURL(/definition/);

  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('the agent page keeps its actions in the top bar, Save changes last', async ({ page }) => {
  await page.goto('/o/acme/f/software/agents/frontend-engineer');
  await expect(page.getByRole('heading', { level: 1, name: 'frontend-engineer' })).toBeVisible();
  if (isPhone(page)) {
    // On a phone only the last action stays in the bar; the others sit behind More actions (#602).
    await page.getByRole('button', { name: 'More actions' }).click();
    await expect(page.getByRole('menuitem', { name: 'Open prompt file' })).toBeVisible();
    await page.keyboard.press('Escape');
  } else {
    await expect(page.getByRole('link', { name: 'Open prompt file' })).toBeVisible();
  }
  await expect(page.getByRole('button', { name: 'Save changes' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Bake off this agent' })).toHaveCount(0);
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

// The pictures for the pull request: this screen at 1440 and 390 px, in both themes. They are
// attached to the report, not compared; the design exports are the reference.
for (const width of [1440, 390] as const) {
  for (const theme of ['light', 'dark'] as const) {
    test(`agent screenshot, ${width}px ${theme}`, async ({ page }, testInfo) => {
      await page.setViewportSize({ width, height: width === 1440 ? 1000 : 844 });
      await page.addInitScript((value) => window.localStorage.setItem('goal-theme', value), theme);
      await page.goto('/o/acme/f/goal/agents/frontend-engineer');
      await expect(page.getByRole('heading', { level: 1, name: 'frontend-engineer' })).toBeVisible();
      await expect(
        page.getByRole('navigation', { name: 'Agent views' }).getByRole('link', { name: /^Runs/ }),
      ).toBeVisible();
      await testInfo.attach(`agent-${width}-${theme}`, {
        body: await page.screenshot({ fullPage: true }),
        contentType: 'image/png',
      });
    });
  }
}
