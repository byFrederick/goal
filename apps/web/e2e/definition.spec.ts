// The Definition editor's frame and Basics (D6) against the mock: goal has a draft of v9 at
// revision 42 based on v8 (3 changes in 2 sections, 1 warning).
import { expect, type Page, test } from '@playwright/test';
import { expectAccessible, expectNoHorizontalScroll, isPhone, openNavigation, orgNav } from './support';

const basics = '/o/acme/f/goal/definition/basics';
const draft = '/v1/orgs/acme/factories/goal/draft';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

const status = (page: Page) => page.getByRole('status').filter({ hasText: /Based on|runs now/ });
const title = (page: Page) => page.getByRole('textbox', { name: 'Title' });
const tile = (page: Page, name: RegExp) =>
  page.getByRole('navigation', { name: 'Definition sections' }).getByRole('link', { name });

/** Floor, from the sidebar or, on a phone, the tab bar. */
const floorLink = (page: Page) =>
  page
    .getByRole('navigation', { name: isPhone(page) ? 'Tabs' : 'Organization and factories' })
    .getByRole('link', { name: 'Floor' });

async function openBasics(page: Page) {
  await page.goto(basics);
  await expect(page.getByRole('heading', { level: 1, name: 'Definition' })).toBeVisible();
  await expect(title(page)).toHaveValue('Goal');
}

test('the definition frame is accessible and fits the screen', async ({ page }) => {
  await openBasics(page);
  await expect(page).toHaveTitle('Basics · Definition · goal · Acme · Goal');
  await expect(page.getByRole('region', { name: 'Draft' })).toContainText('Draft of v9, 3 changes');
  await expect(status(page)).toContainText('Based on v8, the version running now.');
  await expect(page.getByRole('link', { name: '3 changes in 2 sections' })).toBeVisible();
  await expect(tile(page, /^Basics, No changes$/)).toHaveAttribute('aria-current', 'location');
  await expect(tile(page, /^Agents, 1 change, 1 problem$/)).toBeAttached();
  await expect(tile(page, /^Review and publish, Fix 1 problem first$/)).toBeAttached();
  await expect(page.getByRole('group', { name: 'Editor view' }).getByRole('button', { name: 'Form' })).toHaveAttribute(
    'aria-pressed',
    'true',
  );
  await expect(page.getByRole('textbox', { name: 'Name' })).toHaveValue('goal');
  await expect(page.getByText('6f0c2a7e-3d1b-4c55-9a7e-1f2b3c4d5e60')).toBeVisible();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);

  // Moving between sections focuses the section's heading.
  await tile(page, /^Line, 2 changes$/).click();
  await expect(page).toHaveURL(/\/definition\/line$/);
  await expect(page.getByRole('heading', { level: 2, name: 'Line', exact: true })).toBeFocused();
  await expect(page.getByRole('list', { name: 'Stations, in order' })).toBeVisible();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
});

test('Definition opens the Line from the sidebar or the drawer', async ({ page }) => {
  await page.goto('/o/acme/f/goal');
  if (isPhone(page)) {
    // The drawer's factory group lists Definition under the open factory.
    await openNavigation(page);
    await orgNav(page).getByRole('link', { name: 'Definition' }).click();
  } else {
    const entry = orgNav(page).getByRole('link', { name: 'Definition' });
    await expect(entry).toContainText('v8 + draft');
    await entry.click();
  }
  await expect(page).toHaveURL(/\/o\/acme\/f\/goal\/definition\/line$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Definition' })).toBeFocused();
});

test('Basics: changes save on their own, count on the tile and survive a reload', async ({ page }) => {
  await openBasics(page);

  await title(page).fill('Goal platform');
  const addType = page.getByRole('textbox', { name: 'Add a work type' });
  await addType.fill('Docs');
  await addType.press('Enter');
  await expect(page.getByRole('alert')).toHaveText(/lowercase letters, digits and dashes/);
  await addType.fill('docs');
  await addType.press('Enter');
  await page.getByRole('button', { name: 'Remove work type design' }).click();

  await expect(status(page)).toContainText('Autosaved');
  await expect(tile(page, /^Basics, [2-9] changes$/)).toBeAttached();
  await expect(page.getByRole('link', { name: /^\d+ changes in 3 sections$/ })).toBeVisible();

  await page.reload();
  await expect(title(page)).toHaveValue('Goal platform');
  const types = page.getByRole('group', { name: 'Work types' });
  await expect(types.getByRole('button', { name: 'Remove work type docs' })).toBeVisible();
  await expect(types.getByRole('button', { name: 'Remove work type design' })).toHaveCount(0);

  // Edited · Undo puts the running version's title back.
  await page.getByRole('button', { name: 'Undo the change to the title' }).click();
  await expect(title(page)).toHaveValue('Goal');
});

test('leaving with a change goald has not saved asks first, and Retry saves it', async ({ page, request }) => {
  await request.post('/__mock/fail', {
    data: { path: '/draft/model/basics', method: 'PUT', status: 503, times: 0 },
  });
  await openBasics(page);

  await title(page).fill('Goal 2');
  await expect(status(page)).toContainText('Not saved: goald failed.');
  await floorLink(page).click();

  const dialog = page.getByRole('dialog', { name: 'Your last changes aren’t saved yet' });
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText('goald failed to save them.');
  await expectAccessible(page);

  await request.delete('/__mock/fail');
  await dialog.getByRole('button', { name: 'Retry' }).click();
  await expect(page).toHaveURL(/\/o\/acme\/f\/goal$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Goal floor' })).toBeVisible();

  await page.goto(basics);
  await expect(title(page)).toHaveValue('Goal 2');
});

test('a field changed here and elsewhere asks which value to keep', async ({ page, request }) => {
  await openBasics(page);

  await title(page).fill('Mine');
  // Another tab saves the same field before this one's autosave goes out.
  const model = await (await request.get(`${draft}/model`)).json();
  const saved = await request.put(`${draft}/model/basics`, {
    data: { value: { ...model.model.basics, title: 'Theirs' } },
    headers: { 'If-Match': `"${model.etags.basics}"` },
  });
  expect(saved.ok()).toBe(true);

  const dialog = page.getByRole('dialog', { name: 'Changed here and elsewhere' });
  await expect(dialog).toBeVisible();
  const row = dialog.getByRole('group', { name: /^Title also changed elsewhere: theirs Theirs, yours Mine$/ });
  await expect(row).toBeVisible();
  await expectAccessible(page);
  await row.getByRole('radio', { name: 'Keep theirs' }).check();
  await dialog.getByRole('button', { name: 'Save these choices' }).click();

  await expect(dialog).toBeHidden();
  await expect(title(page)).toHaveValue('Theirs');
  await expect(status(page)).not.toContainText('Not saved');
});

test('discard the draft: the running version shows, and the next change opens a new draft', async ({ page }) => {
  await openBasics(page);
  const review = page.getByRole('link', { name: 'Review changes', exact: true }).first();
  await expect(review).not.toHaveAccessibleDescription('No changes yet');

  await page.getByRole('button', { name: 'Discard draft', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Discard the draft of v9?' });
  await expect(dialog).toContainText('3 changes in 2 sections are lost. v8 keeps running.');
  await dialog.getByRole('button', { name: 'Discard draft' }).click();

  await expect(dialog).toBeHidden();
  await expect(status(page)).toContainText('v8 runs now. Your first change opens a draft.');
  await expect(page.getByText('Live: v8', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Discard draft', exact: true })).toHaveCount(0);
  await expect(tile(page, /^Line, No changes$/)).toBeAttached();
  // Nothing to publish: Review changes is not the page's primary action.
  await expect(review).toHaveAccessibleDescription('No changes yet');

  await title(page).fill('Fresh start');
  await expect(status(page)).toContainText('Based on v8, the version running now.');
  await expect(status(page)).toContainText('Autosaved');
  await expect(page.getByRole('region', { name: 'Draft' })).toContainText('Draft of v9');
  await expect(tile(page, /^Basics, 1 change$/)).toBeAttached();
  await expect(review).not.toHaveAccessibleDescription('No changes yet');
});

test('a draft discarded elsewhere keeps the changes here for a new draft', async ({ page, request }) => {
  await openBasics(page);

  await request.delete(draft, { headers: { 'If-Match': '"42"' } });
  const dialog = page.getByRole('dialog', { name: 'This draft was discarded in another tab or the CLI' });
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: 'Start a new draft' }).click();

  await title(page).fill('After the discard');
  await expect(status(page)).toContainText('Autosaved');
  await expect(tile(page, /^Basics, 1 change$/)).toBeAttached();
});

test('the YAML editor at 375 px: the file tree waits behind Files, and the Outline is left out', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 800 });
  await page.goto('/o/acme/f/goal/definition/line?mode=yaml');
  const files = page.getByRole('button', { name: 'Files', exact: true });
  await expect(files).toHaveAttribute('aria-expanded', 'false');
  await expect(page.getByRole('navigation', { name: 'Definition files' })).toBeHidden();
  await expect(page.getByRole('complementary', { name: 'Outline' })).toBeHidden();
  await expectNoHorizontalScroll(page);

  await files.click();
  await expect(page.getByRole('navigation', { name: 'Definition files' })).toBeVisible();
  await expectAccessible(page);
});
