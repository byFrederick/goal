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

test('the floor shows the cards below the figures: Needs you, where work waits, throughput and activity', async ({
  page,
}) => {
  await page.goto('/o/acme/f/sre');
  await expect(page.getByRole('heading', { level: 1, name: 'Floor' })).toBeVisible();

  const needs = page.getByRole('region', { name: 'Needs you' });
  await expect(needs.getByRole('link', { name: isPhone(page) ? 'See all' : 'Open all' })).toHaveAttribute(
    'href',
    '/o/acme/f/sre/items',
  );
  await expect(needs.getByText('Question', { exact: true })).toHaveCount(0); // agents cannot ask yet

  const waits = page.getByRole('region', { name: 'Where work waits' });
  await expect(waits.getByText("Share of the week each station's agents were busy")).toBeVisible();
  await expect(waits.getByRole('button', { name: /^Add (a|an) .+ agent$/ })).toHaveAttribute('aria-disabled', 'true');

  const weeks = page.getByRole('region', { name: 'Throughput by week' });
  await expect(weeks.getByRole('img', { name: /^Items merged a day by week, from .+ Target 8\.$/ })).toBeVisible();
  await expect(weeks.getByText('This week')).toBeVisible();

  const activity = page.getByRole('region', { name: 'Activity' });
  await expect(activity.getByText('Live', { exact: true })).toBeVisible();
  await expect(activity.getByRole('listitem').first()).toBeVisible();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('Needs you approves a decision from its row and sends a reason back', async ({ page }) => {
  test.skip(isPhone(page), 'the phone row sends the person to the item to approve');
  await page.goto('/o/acme/f/sre');

  const needs = page.getByRole('region', { name: 'Needs you' });
  const approve = needs.getByRole('button', { name: /^Approve / }).first();
  if ((await approve.count()) === 0) test.skip(true, 'the seeded factory has no decision waiting');
  await approve.click();
  await expect(page.getByText(/^Approved /)).toBeVisible();

  await needs.getByRole('button', { name: 'Deny' }).first().click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('textbox').fill('The tests miss the empty state');
  await dialog.getByRole('button', { name: /send back/i }).click();
  await expect(page.getByText(/^Sent .+ back\.$/)).toBeVisible();
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
