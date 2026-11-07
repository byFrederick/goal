// The model key, end to end against the mock's engine (assembly: onboarding sets up the model key). A new
// organization sets the key in the Line step's Models card and publishes: the starter item is claimed and runs
// instead of blocking with model_key_missing. Without a key the First item step files nothing. The project
// decides the width: phone-375, tablet-768 and desktop-1440. Not run in the box: Playwright needs a browser,
// which is not installed here. CI runs it against the mock.
import { expect, type Page, test } from '@playwright/test';
import { expectNoHorizontalScroll } from './support';

interface Card {
  id: string;
  state: string;
}

async function newOrganization(page: Page) {
  const reset = await page.request.post('/__mock/reset', {
    data: { orgs: 'none', auth: { mode: 'session', signed_in: 'fresh', email: 'f.cruz@example.com' } },
  });
  expect(reset.ok()).toBeTruthy();
  await page.goto('/get-started');
  await page.getByLabel('Organization name').fill('Acme');
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page).toHaveURL(/\/o\/acme\/get-started\/factory$/);
}

/** Step 3 without code, and the Line step reached. */
async function reachLine(page: Page) {
  await page.getByRole('radio', { name: /^No code/ }).check();
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page).toHaveURL(/\/o\/acme\/get-started\/line\?.*factory=/);
  await expect(page.getByRole('heading', { level: 1, name: 'Shape the line' })).toBeVisible();
}

/** Pastes a key in the Models card and saves it. */
async function setModelKey(page: Page) {
  const next = page.getByRole('button', { name: 'Continue' });
  await expect(next).toBeDisabled();
  await expect(page.getByText('Add the model key, then continue.')).toBeVisible();
  await page
    .locator('input')
    .and(page.getByLabel('Model key', { exact: true }))
    .fill('test-token');
  await page.getByRole('button', { name: 'Save the key' }).click();
  await expect(page.getByText('Saved as the secret model-key, open to this factory.')).toBeVisible();
  await expect(next).toBeEnabled();
}

/** Line to First item: Budget publishes, then Intake and People are passed. */
async function reachFirstItem(page: Page) {
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Set budgets and targets' })).toBeVisible();
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page).toHaveURL(/\/o\/acme\/get-started\/intake\?.*factory=/);
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page).toHaveURL(/\/o\/acme\/get-started\/people\?.*factory=/);
  await page.getByRole('link', { name: 'Skip for now' }).click();
  await expect(page).toHaveURL(/\/o\/acme\/get-started\/first-item\?.*factory=/);
}

const factoryOf = (page: Page) => new URL(page.url()).searchParams.get('factory') ?? '';

/** The starter item runs instead of blocking: it is claimed, has a run and is not blocked. */
async function expectItemRuns(page: Page) {
  await expect(page.getByText('Created from the setup')).toBeVisible();
  await expectNoHorizontalScroll(page);
  const factory = factoryOf(page);
  const list = await page.request.get(`/v1/orgs/acme/factories/${factory}/items`);
  expect(list.ok()).toBeTruthy();
  const { items } = (await list.json()) as { items: Card[] };
  expect(items).toHaveLength(1);
  const [item] = items as [Card];
  expect(['in_progress', 'done']).toContain(item.state);

  const events = await page.request.get(`/v1/orgs/acme/items/${item.id}/events`);
  const kinds = ((await events.json()) as { events: { kind: string }[] }).events.map((e) => e.kind);
  expect(kinds).toContain('claimed');
  expect(kinds).not.toContain('blocked');
  const runs = await page.request.get(`/v1/orgs/acme/items/${item.id}/runs`);
  expect(((await runs.json()) as { runs: unknown[] }).runs.length).toBeGreaterThan(0);
}

test('a template with a model key set in the Line step starts its first item and runs it', async ({ page }) => {
  await newOrganization(page);
  // Incident response allows No code; the first template (a code review) needs at least Read only.
  await page.getByRole('radio', { name: /Incident response/ }).check();
  await page.getByRole('button', { name: /^Use / }).click();
  await expect(page).toHaveURL(/\/o\/acme\/get-started\/code\?.*factory=/);
  await reachLine(page);
  await expectNoHorizontalScroll(page);
  await setModelKey(page);
  await reachFirstItem(page);
  await expectItemRuns(page);
});

// A Start empty factory has no starter item (goald's authoring/create.go and the mock's blankSkeleton), so the
// First item step shows its empty state and files nothing. The key is proven by filing a starter item by hand.
test('Start empty with a model key set in the Line step shows no hold, and an item filed later runs', async ({
  page,
}) => {
  await newOrganization(page);
  await page.getByRole('radio', { name: /Start empty/ }).check();
  await page.getByRole('button', { name: /^Use / }).click();
  await expect(page).toHaveURL(/\/o\/acme\/get-started\/code\?.*factory=/);
  await reachLine(page);
  await setModelKey(page);
  await reachFirstItem(page);
  await expect(page.getByText('This factory has no starter item, so nothing runs yet.')).toBeVisible();
  await expect(page.getByText('Agents need a model key to run.')).toHaveCount(0);
  await expectNoHorizontalScroll(page);

  const factory = factoryOf(page);
  const empty = await page.request.get(`/v1/orgs/acme/factories/${factory}/items`);
  expect(((await empty.json()) as { items: Card[] }).items).toEqual([]);

  const filed = await page.request.post(`/v1/orgs/acme/factories/${factory}/items`, {
    data: { title: 'Review the newest open pull request' },
  });
  expect(filed.ok()).toBeTruthy();
  const { id } = (await filed.json()) as { id: string };
  const events = await page.request.get(`/v1/orgs/acme/items/${id}/events`);
  const kinds = ((await events.json()) as { events: { kind: string }[] }).events.map((e) => e.kind);
  expect(kinds).toContain('claimed');
  expect(kinds).not.toContain('blocked');
  const runs = await page.request.get(`/v1/orgs/acme/items/${id}/runs`);
  expect(((await runs.json()) as { runs: unknown[] }).runs.length).toBeGreaterThan(0);
});

test('without a model key the First item step files nothing and says why', async ({ page }) => {
  await newOrganization(page);
  await page.getByRole('button', { name: /^Use / }).click();
  await expect(page).toHaveURL(/\/o\/acme\/get-started\/code\?.*factory=/);
  const factory = factoryOf(page);

  // Publish through the API, so the step is reached with no key on the line.
  const draft = await page.request.get(`/v1/orgs/acme/factories/${factory}/draft`);
  expect(draft.ok()).toBeTruthy();
  const { revision } = (await draft.json()) as { revision: number };
  const published = await page.request.post(`/v1/orgs/acme/factories/${factory}/draft/publish`, {
    data: { revision, expected_current_version_id: null },
  });
  expect(published.ok()).toBeTruthy();

  await page.goto(`/o/acme/get-started/first-item?factory=${factory}`);
  await expect(
    page.getByText('Agents need a model key to run. Add one here, then your first item starts.'),
  ).toBeVisible();
  await expectNoHorizontalScroll(page);

  const list = await page.request.get(`/v1/orgs/acme/factories/${factory}/items`);
  expect(((await list.json()) as { items: Card[] }).items).toEqual([]);
});

// A published line with no model key: the Code card wraps its sentence at spaces, and Home says what the Floor
// says. Not run in the box (no browser); CI runs it in phone-375 and desktop-1440.
test('a published line without a model key: the Code card wraps at spaces and Home and the Floor say Needs a model key', async ({
  page,
}) => {
  await newOrganization(page);
  await page.getByRole('button', { name: /^Use / }).click();
  await expect(page).toHaveURL(/\/o\/acme\/get-started\/code\?.*factory=/);
  const factory = factoryOf(page);

  await page.getByRole('radio', { name: /^No code/ }).check();
  const detail = page.getByText(/Items don't touch a repository/);
  await expect(detail).toBeVisible();
  expect(await detail.evaluate((el) => getComputedStyle(el).wordBreak)).not.toBe('break-all');
  // "repository" sits on one line: no line of the wrapped text ends inside the word.
  const split = await detail.evaluate((el) => {
    const text = el.firstChild as Text;
    const start = (text.data ?? '').indexOf('repository');
    const range = document.createRange();
    range.setStart(text, start);
    range.setEnd(text, start + 'repository'.length);
    return new Set([...range.getClientRects()].map((r) => Math.round(r.top))).size;
  });
  expect(split).toBe(1);
  await expectNoHorizontalScroll(page);

  const draft = await page.request.get(`/v1/orgs/acme/factories/${factory}/draft`);
  const { revision } = (await draft.json()) as { revision: number };
  const published = await page.request.post(`/v1/orgs/acme/factories/${factory}/draft/publish`, {
    data: { revision, expected_current_version_id: null },
  });
  expect(published.ok()).toBeTruthy();

  await page.goto('/o/acme');
  const row = page.getByRole('article').first();
  await expect(row.getByText('Needs a model key')).toBeVisible();
  await expect(row.getByText('Running')).toHaveCount(0);
  await expectNoHorizontalScroll(page);

  await row.getByRole('link').first().click();
  await expect(page.getByRole('heading', { level: 1, name: 'Floor' })).toBeVisible();
  await expect(page.getByText('Needs a model key').first()).toBeVisible();
});
