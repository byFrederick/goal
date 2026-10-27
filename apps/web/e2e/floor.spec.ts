// The Floor over a range (E4): throughput against the range before and the target, the last 12
// weeks, the header's window control writing the URL, and the line and the flow tiles of the top.
import { expect, type Page, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll, isPhone } from './support';

// One instant for the test, the mock and the page: noon UTC of the day the file loads, so the days
// this test expects are the days goald and the page count from, even when the run crosses UTC
// midnight.
const now = new Date(`${new Date().toISOString().slice(0, 10)}T12:00:00Z`);

test.beforeEach(async ({ page, request }) => {
  await request.post('/__mock/reset', { data: { now: now.toISOString() } });
  await page.clock.setFixedTime(now);
});

const dayMs = 86_400_000;
const utcDay = (offset: number) => new Date(now.getTime() - offset * dayMs).toISOString().slice(0, 10);
const month = new Intl.DateTimeFormat('en-US', { month: 'short', timeZone: 'UTC' });

/** "Oct 9" for a day `offset` days before today. */
function dayLabel(offset: number): string {
  const d = new Date(`${utcDay(offset)}T12:00:00Z`);
  return `${month.format(d)} ${d.getUTCDate()}`;
}

const throughput = (page: Page) => page.getByRole('region', { name: 'Throughput' });

test('the floor shows throughput against last week and its target, and the last 12 weeks', async ({ page }) => {
  await page.goto('/o/acme/f/sre');
  await expect(page.getByRole('heading', { level: 1, name: 'Floor' })).toBeVisible();

  const card = throughput(page);
  await expect(card.getByText(/^\d+\.\d a day$/)).toBeVisible();
  await expect(card.getByText(/vs last week/)).toBeVisible();
  await expect(card.getByRole('meter', { name: /^\d+\.\d a day against a target of 8$/ })).toBeVisible();
  await expect(card.getByText(/^(\d+ (s|min|h|d)( \d+ (min|h))?|Not enough data)$/)).toBeVisible();

  const weeks = page.getByRole('region', { name: 'Last 12 weeks' });
  await expect(weeks.getByRole('img', { name: /^Throughput by week from .+ Target 8\.$/ })).toBeVisible();
  await expect(weeks.getByRole('table', { name: 'Items finished per day, by week' }).getByRole('row')).toHaveCount(13);

  const bridge = page.getByRole('region', { name: 'Why lead time is over target' });
  // Durations read as on the Item page: "7 h 12 min", never "7.2 h" or "0.0 h".
  const duration = String.raw`\d+ (s|min|h|d)( \d+ (min|h))?`;
  await expect(bridge.getByText(new RegExp(`^\\+${duration} over the 6 h target, p85$`))).toBeVisible();
  await expect(
    bridge.getByRole('img', { name: new RegExp(`^Lead time p85 ${duration} against a target of 6 h: `) }),
  ).toBeVisible();
  const steps = bridge.getByRole('table', { name: 'Lead time from target to actual, p85' }).getByRole('row');
  await expect(steps.first()).toContainText('Step');
  await expect(steps.nth(1)).toContainText('Target6 h');
  await expect(steps.last()).toContainText(new RegExp(`^Actual${duration}$`));
  await expect(bridge.getByText(/ explains .+ of the gap\.$/)).toBeVisible();

  if (isPhone(page)) {
    await expect(page.getByText(/, vs the week before$/)).toBeVisible();
  } else {
    await expect(page.getByText(`vs ${dayLabel(13)} to`)).toBeVisible();
  }
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('the constraint card pages through the busiest stations over the range', async ({ page }) => {
  await page.goto('/o/acme/f/sre');

  const card = page.getByRole('region', { name: 'Investigate is holding the line back' });
  await expect(card.getByText('Constraint found')).toBeVisible();
  await expect(card.getByText(/^\d+%$/)).toBeVisible();
  await expect(card.getByText(isPhone(page) ? '7 days' : 'over 7 days', { exact: true })).toBeVisible();
  await expect(
    card.getByText(/A fourth investigator would cut lead time by about \d+ (hours?|minutes?)\.$/),
  ).toBeVisible();
  await expect(card.getByRole('link', { name: 'Add an investigator' })).toHaveAttribute(
    'href',
    '/o/acme/f/sre/definition/line?station=investigate',
  );

  await card.getByRole('button', { name: 'Next candidate' }).click();
  const next = page.getByRole('region', { name: 'Triage is the next busiest' });
  await expect(next.getByText('2 of 2')).toBeVisible();
  await expect(next.getByText(/would cut lead time/)).toHaveCount(0); // no estimate without a queue
  await expect(next.getByRole('button', { name: 'Next candidate' })).toBeDisabled();
  await next.getByRole('button', { name: 'Previous candidate' }).click();
  await expect(page.getByRole('region', { name: 'Investigate is holding the line back' })).toBeVisible();
  await expectAccessible(page);
});

test('the window control writes the URL, and the cards follow it', async ({ page }) => {
  await page.goto('/o/acme/f/sre');
  await expect(page.getByRole('heading', { level: 1, name: 'Floor' })).toBeVisible();

  await page.getByRole('radio', { name: '30 days' }).click();
  await expect(page).toHaveURL(new RegExp(`\\?from=${utcDay(29)}$`));
  await expect(page.getByRole('radio', { name: '30 days' })).toBeChecked();
  await expect(page.getByText('compared with the 30 days before')).toBeVisible();
  await expect(throughput(page).getByText('vs the 30 days before')).toBeVisible();

  await page.getByRole('radio', { name: 'Today' }).click();
  await expect(page).toHaveURL(new RegExp(`\\?from=${utcDay(0)}$`));
  await expect(page.getByText('compared with the day before')).toBeVisible();

  await page.getByRole('radio', { name: '7 days' }).click();
  await expect(page).toHaveURL(/\/o\/acme\/f\/sre\/?$/);
  await expect(page.getByText(`${dayLabel(6)} to`)).toBeVisible();
  await expectAccessible(page);
});

test('the line and the flow tiles show on the Floor', async ({ page }) => {
  await page.goto('/o/acme/f/software');
  await expect(page.getByRole('heading', { level: 1, name: 'Floor' })).toBeVisible();
  await expect(page.getByText('Line running')).toBeVisible();

  const line = page.getByRole('region', { name: 'The line, now' });
  await expect(line.getByRole('list', { name: 'Stations from intake to done' })).toBeVisible();
  await expect(line.getByRole('img', { name: 'Engineer: 5 working, 1 waiting on you, 0 free' })).toBeVisible();
  await expect(line.getByText('Constraint')).toBeVisible();

  const flow = page.getByRole('region', { name: 'Flow against targets' });
  await expect(flow.getByRole('meter', { name: /of the target of 10 a day$/ })).toBeVisible();
  // The phone shows four tiles; Hands-off is one of the two hidden on it.
  test.skip(isPhone(page), 'the phone shows four tiles');
  await expect(flow.getByRole('button', { name: 'Hands-off' })).toHaveAttribute('aria-disabled', 'true');
  await expectAccessible(page);
});

test('a phone shows the range the URL names', async ({ page }) => {
  test.skip(!isPhone(page), 'the caption is the phone header');
  await page.goto(`/o/acme/f/sre?from=${utcDay(29)}`);
  await expect(page.getByText(/, compared with the 30 days before$/)).toBeVisible();
  await expectNoHorizontalScroll(page);
});
