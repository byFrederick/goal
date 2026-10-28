// E12 and build/agent: an agent's statistics, why review sent its work back, and its tabs (?view=).
import { expect, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll } from './support';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

test('the agent page shows the last 7 days, its station and why work was sent back', async ({ page }) => {
  await page.goto('/o/acme/f/sre/agents/investigator');
  await expect(page.getByRole('heading', { level: 1, name: 'investigator' })).toBeVisible();
  await expect(page).toHaveTitle(/^investigator · sre · Acme · Goal$/);
  await expect(page.getByText('Investigate station')).toBeVisible();

  const measures = page.getByRole('region', { name: 'Last 7 days' });
  await expect(measures.getByText('31', { exact: true })).toBeVisible();
  await expect(measures.getByText('22%', { exact: true })).toBeVisible();
  await expect(measures.getByText('$0.94')).toBeVisible();
  await expect(measures.getByText('24 min')).toBeVisible();

  const reasons = page.getByRole('region', { name: 'Why review sent work back' });
  await expect(reasons.getByRole('listitem')).toHaveCount(3);
  await expect(reasons.getByRole('listitem').first()).toContainText('No CloudWatch evidence for a latency claim');
  await expect(reasons.getByRole('listitem').first()).toContainText('4×');

  await page.getByRole('navigation', { name: 'Agent views' }).getByRole('link', { name: 'Tools and access' }).click();
  await expect(page.getByText('Blocked this week: 2 calls, both aws ssm put-parameter.')).toBeVisible();

  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
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

  // Ask Jonah for a fix asks about this agent and opens the definition on that request (ADR 0021).
  const askJonah = page.getByRole('button', { name: 'Ask Jonah for a fix' });
  await expect(askJonah).toBeEnabled();
  await askJonah.click();
  await expect(page).toHaveURL(/\/o\/acme\/f\/sre\/definition\/agents\?.*request=/);
  await expect(page.getByRole('textbox', { name: 'Ask the factory to change its definition' })).toBeDisabled();
  await expect(page.getByText('Done')).toBeVisible({ timeout: 10_000 });

  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('Bake off this agent starts a bake-off and opens the new one', async ({ page }) => {
  await page.goto('/o/acme/f/software/agents/frontend-engineer');
  await expect(page.getByRole('heading', { level: 1, name: 'frontend-engineer' })).toBeVisible();

  await page.getByRole('button', { name: 'Bake off this agent' }).click();

  await expect(page).toHaveURL(/\/o\/acme\/f\/software\/jonah\/bake-offs\/bake-off-\d+$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Which model for frontend-engineer?' })).toBeVisible();
  await expect(page.getByText('Queued')).toBeVisible();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});
