// Set lane (the Items bulk bar, ADR 0016 decision 4): a person checks two items, sets their lane from
// the menu and reads the result, at 375, 768 and 1440 px and in dark mode. Not run in the box:
// Playwright needs a browser, which the box does not have.
import { type APIRequestContext, expect, type Page, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll, expectToast, recordToasts } from './support';

const widths = [375, 768, 1440] as const;
const goal = '/v1/orgs/acme/factories/goal';

test.beforeEach(async ({ page, request }) => {
  await request.post('/__mock/reset');
  await recordToasts(page);
});

/** Creates an open item with no lane and returns its id. */
async function newItem(request: APIRequestContext, title: string): Promise<string> {
  const created = await request.post(`${goal}/items`, { data: { title } });
  return ((await created.json()) as { id: string }).id;
}

/** Checks the rows of the two titles and sets their lane from the bulk bar's Set lane menu. */
async function setLaneOfTwo(page: Page, request: APIRequestContext) {
  const ids = [await newItem(request, 'Set lane first'), await newItem(request, 'Set lane second')];
  await page.goto('/o/acme/f/goal/items');
  const bar = page.getByRole('region', { name: 'Bulk actions' });
  // The bar opens after the table and sticks to the bottom edge, so it never moves a row; each check still
  // waits for the bar's count, because the selection lives in the URL.
  const titles = ['Set lane first', 'Set lane second'];
  for (const [at, title] of titles.entries()) {
    // click, not check: the selection lives in the URL, so the box changes after the router's navigation,
    // and check() reads its state right after the click and fails when the box has not caught up yet.
    const box = page.getByRole('row').filter({ hasText: title }).getByRole('checkbox');
    await box.click();
    await expect(box).toBeChecked();
    await expect(bar.getByText(`${at + 1} selected`)).toBeVisible();
  }
  await expect(bar.getByText('2 selected')).toBeVisible();
  await bar.getByRole('button', { name: 'Set lane' }).click();

  const dialog = page.getByRole('dialog', { name: 'Set lane for 2 items' });
  await expect(dialog).toBeVisible();
  await dialog.getByRole('radio', { name: 'Frontend' }).check();
  await dialog.getByRole('button', { name: 'Set lane to Frontend' }).click();

  await expect(dialog).toBeHidden();
  // The pointer rests where the dialog's button was, which is now over a row's ⓘ: its open tip would cover
  // the buttons beside it and axe would measure them as under 24 px. A person moves the pointer on.
  await page.mouse.move(0, 0);
  await expectToast(page, 'Set the lane of 2 items to Frontend.');
  for (const id of ids) {
    const item = await (await request.get(`/v1/orgs/acme/items/${id}`)).json();
    expect(item.lane).toBe('frontend');
  }
}

for (const width of widths) {
  test(`set the lane of two checked items at ${width} px, then read the result`, async ({ page, request }) => {
    await page.setViewportSize({ width, height: 900 });
    await setLaneOfTwo(page, request);

    await expectAccessible(page);
    await expectNoHorizontalScroll(page);
  });
}

test('set the lane of two checked items in dark mode', async ({ page, request }) => {
  // The same height as the widths above: on a shorter page a row's ⓘ can sit under the sticky bulk bar, and axe
  // measures it as partly covered.
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.emulateMedia({ colorScheme: 'dark' });
  await setLaneOfTwo(page, request);

  await expectAccessible(page);
});
