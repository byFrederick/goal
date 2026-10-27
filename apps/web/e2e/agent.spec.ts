// E12: an agent's statistics and why review sent its work back (Agent and Mobile-Agent boards).
import { expect, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll } from './support';

/** The page lays out by the room beside the sidebar (256 px from 768 px): the five
 * tiles of the Agent board from 720 px of it, the four of Mobile-Agent below. */
const wide = (width: number) => width >= 768 && width - 304 >= 720;

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

test('the agent page shows the last 7 days and why work was sent back', async ({ page }) => {
  await page.goto('/o/acme/f/sre/agents/investigator');
  await expect(page.getByRole('heading', { level: 1, name: 'investigator' })).toBeVisible();
  await expect(page).toHaveTitle(/^investigator · sre · Acme · Goal$/);
  await expect(
    page.getByText('Three copies run at Investigate, the current constraint, on claude-sonnet-5-5.'),
  ).toBeVisible();

  const five = wide(page.viewportSize()?.width ?? 0);
  const stat = (label: string) =>
    page.getByRole('term').filter({ hasText: label }).locator('xpath=..').getByRole('definition');
  await expect(stat('Tasks finished')).toHaveText('31');
  await expect(stat('Sent back')).toHaveText('22%');
  await expect(stat('Spend per task')).toHaveText('$0.94');
  await expect(stat(five ? 'Time per task, p85' : 'Time, p85')).toHaveText('24 min');
  // Mobile-Agent shows four tiles: no median turns in a narrow page.
  await expect(stat('Median turns')).toHaveCount(five ? 1 : 0);
  if (five) await expect(stat('Median turns')).toHaveText('18');

  const reasons = page.getByRole('region', { name: 'Why review sent work back' });
  await expect(reasons.getByRole('listitem')).toHaveCount(3);
  await expect(reasons.getByRole('listitem').first()).toContainText('No CloudWatch evidence for a latency claim');
  await expect(reasons.getByRole('listitem').first()).toContainText('4×');
  await expect(page.getByText('Blocked this week: 2 calls, both aws ssm put-parameter.')).toBeVisible();

  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('an agent without history says so, and an unknown agent is not found', async ({ page }) => {
  await page.goto('/o/acme/f/goal/agents/reviewer');
  await expect(page.getByRole('heading', { level: 1, name: 'reviewer' })).toBeVisible();
  await expect(page.getByText('Nothing was sent back in the last 7 days.')).toBeVisible();
  await expect(page.getByText('Blocked this week: no calls.')).toBeVisible();

  await page.goto('/o/acme/f/goal/agents/nobody');
  await expect(page.getByRole('heading', { level: 1, name: 'No such agent' })).toBeVisible();
});
