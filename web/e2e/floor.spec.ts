// The Floor over a range (E4): throughput against the range before and the target, the last 12
// weeks, and the header's range and work-type controls writing the URL.
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
  await expect(page.getByRole('heading', { level: 1, name: 'SRE floor' })).toBeVisible();

  const card = throughput(page);
  await expect(card.getByText(/^\d+\.\d a day$/)).toBeVisible();
  await expect(card.getByText(/vs last week/)).toBeVisible();
  await expect(card.getByRole('meter', { name: /^\d+\.\d a day against a target of 8$/ })).toBeVisible();
  await expect(card.getByText(/^(\d+(\.\d+)? h|Not enough data)$/)).toBeVisible();

  const weeks = page.getByRole('region', { name: 'Last 12 weeks' });
  await expect(weeks.getByRole('img', { name: /^Throughput by week from .+ Target 8\.$/ })).toBeVisible();
  await expect(weeks.getByRole('table', { name: 'Items finished per day, by week' }).getByRole('row')).toHaveCount(13);

  const bridge = page.getByRole('region', { name: 'Why lead time is over target' });
  await expect(bridge.getByText(/^\+\d+\.\d h over the 6 h target, p85$/)).toBeVisible();
  await expect(bridge.getByRole('img', { name: /^Lead time p85 \d+\.\d h against a target of 6 h: / })).toBeVisible();
  const steps = bridge.getByRole('table', { name: 'Lead time from target to actual, p85' }).getByRole('row');
  await expect(steps.first()).toContainText('Step');
  await expect(steps.nth(1)).toContainText('Target6.0 h');
  await expect(steps.last()).toContainText(/^Actual\d+\.\d h$/);
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

test('a range and a work type change the URL and the cards', async ({ page }) => {
  test.skip(isPhone(page), 'phones show the range the URL names; the controls are on wider screens');
  await page.goto('/o/acme/f/sre');
  await expect(page.getByRole('heading', { level: 1, name: 'SRE floor' })).toBeVisible();

  await page.getByRole('button', { name: new RegExp(`^${dayLabel(6)} to `) }).click();
  await page.getByRole('button', { name: 'Last 14 days' }).click();
  await expect(page).toHaveURL(new RegExp(`\\?from=${utcDay(13)}$`));
  await expect(throughput(page).getByText('vs the 14 days before')).toBeVisible();
  await expect(throughput(page).getByText(`Items finished per day, ${dayLabel(13)} to`)).toBeVisible();

  await page.getByRole('button', { name: /^[A-Z][a-z]{2} \d+ to / }).click();
  const panel = page.getByRole('dialog', { name: 'Date range' });
  await panel.getByLabel('From').fill(utcDay(40));
  await panel.getByLabel('To').fill(utcDay(10));
  await panel.getByRole('button', { name: 'Show these days' }).click();
  await expect(page).toHaveURL(new RegExp(`from=${utcDay(40)}&to=${utcDay(10)}`));
  await expect(throughput(page).getByText('vs the 31 days before')).toBeVisible();

  await page.getByRole('button', { name: /^[A-Z][a-z]{2} \d+ to / }).click();
  await panel.getByLabel('From').fill(utcDay(120));
  await panel.getByRole('button', { name: 'Show these days' }).click();
  await expect(panel.getByRole('alert')).toHaveText('A range is at most 90 days.');
  await page.keyboard.press('Escape');

  await page.getByRole('button', { name: 'Work type: all work types' }).click();
  await page.getByRole('menuitemradio', { name: 'incident' }).click();
  await expect(page).toHaveURL(/type=incident/);
  await expect(page).toHaveURL(new RegExp(`from=${utcDay(40)}`));
  await expect(page.getByRole('button', { name: 'Work type: incident' })).toBeVisible();
  await expectAccessible(page);
});

test('a phone shows the range the URL names', async ({ page }) => {
  test.skip(!isPhone(page), 'the caption is the phone header');
  await page.goto(`/o/acme/f/sre?from=${utcDay(29)}&type=incident`);
  await expect(page.getByText(/, vs the 30 days before, incident only$/)).toBeVisible();
  await expectNoHorizontalScroll(page);
});
