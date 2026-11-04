// Knowledge (ADR 0038 decision 12) against the mock: the sources list with Find a file and Most read,
// Add a source (Upload, Repository, Confluence, the Check and the factory checkboxes), and a source's
// page with its files, latest sync, settings, the line agents see and who reads it. The mock starts with
// acme's sources (dashboards and product-specs, whose last syncs failed; goal-docs; and support-macros,
// which syncs only on demand) and no upload
// source, so a flow that needs one creates `handbook` first. Not run in the build box (no browser).
import { type APIRequestContext, expect, type Page, test } from '@playwright/test';
import { status } from './editor';
import { expectAccessible, expectNoHorizontalScroll, isPhone, orgNav, recordToasts } from './support';

test.beforeEach(async ({ page, request }) => {
  await request.post('/__mock/reset');
  await recordToasts(page);
});

async function fits(page: Page) {
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
}

/** Creates an upload source through the API, as Add a source does. */
async function createUploadSource(request: APIRequestContext, name: string) {
  const res = await request.post('/v1/orgs/acme/knowledge/sources', {
    data: { name, title: name, kind: 'upload', read_when: 'Read before answering about policy.' },
  });
  expect(res.status()).toBe(201);
}

test.describe('the Knowledge page', () => {
  test('lists each source with its read-when line, kind and sync, and says which one failed', async ({ page }) => {
    await page.goto('/o/acme/knowledge');
    await expect(page).toHaveTitle('Knowledge · Acme · Goal');
    await expect(page.getByRole('heading', { level: 1, name: 'Knowledge' })).toBeVisible();
    await expect(page.getByRole('region', { name: 'Sources', exact: true })).toBeVisible();
    await expect(page.getByRole('alert').filter({ hasText: 'dashboards' })).toContainText('couldn’t sync');
    await expect(page.getByRole('link', { name: 'goal-docs', exact: true }).first()).toBeVisible();
    await fits(page);
  });

  test('Find a file looks across sources by path or title, and a hit opens its source', async ({ page }) => {
    await page.goto('/o/acme/knowledge');
    await page.getByRole('searchbox', { name: 'Find a file' }).fill('a');
    await expect(page).toHaveURL(/q=a/);
    const hits = page.getByRole('list', { name: 'Files found' });
    await expect(hits.getByRole('link').first()).toBeVisible();
    await hits.getByRole('link').first().click();
    await expect(page).toHaveURL(/\/o\/acme\/knowledge\/[a-z-]+\?find=/);
  });

  test('a factory the organization has no factory of is said, never the unfiltered list', async ({ page }) => {
    await page.goto('/o/acme/knowledge?factory=nope');
    await expect(page.getByText('Acme has no factory named nope.')).toBeVisible();
  });

  test("the sidebar's Knowledge opens the organization's sources", async ({ page }) => {
    test.skip(isPhone(page), 'the sidebar is the 768 px and wider navigation');
    await page.goto('/o/acme/f/sre');
    await orgNav(page).getByRole('link', { name: 'Knowledge' }).click();
    await expect(page).toHaveURL(/\/o\/acme\/knowledge$/);
    await expect(page.getByRole('region', { name: 'Sources', exact: true })).toBeVisible();
  });

  test("on a phone, More opens the drawer, whose Knowledge is the organization's", async ({ page }) => {
    test.skip(!isPhone(page), 'phone navigation');
    await page.goto('/o/acme/f/sre');
    await page.getByRole('navigation', { name: 'Tabs' }).getByRole('button', { name: 'More' }).click();
    await page.getByRole('dialog', { name: 'Acme' }).getByRole('link', { name: 'Knowledge' }).click();
    await expect(page).toHaveURL(/\/o\/acme\/knowledge$/);
  });
});

test.describe('a source', () => {
  test('shows its files with their reads, the latest sync, the line agents see and who reads it', async ({ page }) => {
    await page.goto('/o/acme/knowledge/goal-docs');
    await expect(page.getByRole('heading', { level: 1, name: /goal-docs/ })).toBeVisible();
    await expect(page.getByRole('table', { name: 'Files of goal-docs' })).toBeVisible();
    await expect(page.getByRole('region', { name: 'Latest sync' })).toBeVisible();
    await expect(page.getByRole('region', { name: 'What agents see' })).toContainText('/knowledge/goal-docs');
    await expect(page.getByRole('region', { name: 'Read by' })).toBeVisible();
    await fits(page);
  });

  test('filters its files by path, kept in the URL', async ({ page }) => {
    await page.goto('/o/acme/knowledge/goal-docs');
    await page.getByRole('searchbox', { name: 'Filter by path' }).fill('zzz');
    await expect(page).toHaveURL(/find=zzz/);
    await expect(page.getByText('No file matches “zzz”.')).toBeVisible();
  });

  test('Add to a factory writes that factory’s draft and says to publish it there', async ({ page }) => {
    await page.goto('/o/acme/knowledge/goal-docs');
    await page.getByRole('button', { name: 'Add to a factory' }).click();
    await page.getByRole('checkbox', { name: /sre/i }).check();
    await page.getByRole('button', { name: 'Add to drafts' }).click();
    await expect(page.getByText(/Listed goal-docs in 1 draft/)).toBeVisible();
  });
});

test.describe('Add a source', () => {
  test('offers the three kinds and shows the others as later', async ({ page }) => {
    await page.goto('/o/acme/knowledge/new');
    const kinds = page.getByRole('group', { name: 'Where the files are' });
    await expect(kinds.getByRole('radio', { name: /Upload/ })).toBeChecked();
    await expect(kinds.getByRole('radio', { name: /Website/ })).toBeDisabled();
    await expect(kinds.getByRole('radio', { name: /Notion/ })).toBeDisabled();
    await expect(kinds.getByRole('radio', { name: /Google Drive/ })).toBeDisabled();
    await fits(page);
  });

  test('upload: a name, when to read it and a factory, then the source exists with its draft written', async ({
    page,
  }) => {
    await page.goto('/o/acme/knowledge/new');
    await page.getByLabel('Name', { exact: true }).fill('handbook');
    await page.getByLabel('When should agents read it?').fill('How Acme works. Read when unsure of a rule.');
    await page.getByRole('checkbox', { name: /sre/i }).check();
    await page.getByRole('button', { name: 'Check' }).click();
    await expect(page.getByText('/knowledge/handbook —')).toBeVisible();
    await page.getByRole('button', { name: 'Add source' }).click();
    await expect(page).toHaveURL(/\/o\/acme\/knowledge\/handbook/);
  });

  test('repository: its settings show, and a name that is taken says so', async ({ page, request }) => {
    await createUploadSource(request, 'handbook');
    await page.goto('/o/acme/knowledge/new?kind=repository');
    await expect(page.getByLabel('Repository')).toBeVisible();
    await page.getByLabel('Name', { exact: true }).fill('handbook');
    await expect(page.getByText('Acme already has a source named handbook.')).toBeVisible();
  });

  test('confluence: its settings show', async ({ page }) => {
    await page.goto('/o/acme/knowledge/new?kind=confluence');
    await expect(page.getByLabel('Site')).toBeVisible();
    await expect(page.getByLabel('Spaces')).toBeVisible();
    await fits(page);
  });
});

test('the builder says while the organization’s sources load, never that it has none', async ({ page, request }) => {
  await request.post('/__mock/fail', { data: { path: '/knowledge/sources', method: 'GET', delay_ms: 2500, times: 0 } });
  await page.goto('/o/acme/f/sre/definition/connections');
  const knowledge = page.getByRole('region', { name: 'Knowledge sre may search' });
  await expect(knowledge.getByText('Loading Acme’s knowledge sources…')).toBeVisible();
  await expect(knowledge.getByText(/has no knowledge sources yet/)).toHaveCount(0);
  await expect(knowledge.getByRole('checkbox', { name: /^Dashboards/ })).toBeChecked({ timeout: 10_000 });
});

test('the builder lists the organization’s sources to tick for the factory', async ({ page }) => {
  await page.goto('/o/acme/f/sre/definition/connections');
  const knowledge = page.getByRole('region', { name: 'Knowledge sre may search' });
  await expect(knowledge.getByRole('checkbox', { name: /^Dashboards/ })).toBeChecked();
  await expect(knowledge.getByRole('checkbox', { name: /^goal docs/ })).not.toBeChecked();
  await expect(knowledge).toContainText('Synced, github, acme/goal main, docs/**/*.md');
  await knowledge.getByRole('checkbox', { name: /^Support macros/ }).check();
  await expect(page.getByRole('region', { name: 'What runs get' })).toContainText('knowledge__search over 2 sources');
  await expect(status(page)).toContainText('Autosaved');
  await page.reload();
  await expect(
    page.getByRole('region', { name: 'Knowledge sre may search' }).getByRole('checkbox', { name: /^Support macros/ }),
  ).toBeChecked();
  await fits(page);
});

// The pictures of the list, a source and New source at 1440 and 390 px in both themes. The Playwright
// suite cannot run in the build box, so the baselines are made by the first CI run with --update-snapshots.
for (const theme of ['light', 'dark'] as const) {
  for (const width of [1440, 390]) {
    const screens = [
      { name: 'list', path: '/o/acme/knowledge', heading: 'Knowledge' },
      { name: 'source', path: '/o/acme/knowledge/goal-docs', heading: 'goal-docs' },
      { name: 'new', path: '/o/acme/knowledge/new', heading: 'New source' },
      { name: 'new-confluence', path: '/o/acme/knowledge/new?kind=confluence', heading: 'New source' },
    ];
    for (const { name, path, heading } of screens) {
      test(`knowledge ${name} screenshot, ${theme}, ${width} px`, async ({ page }) => {
        await page.addInitScript((value) => window.localStorage.setItem('goal-theme', value), theme);
        await page.setViewportSize({ width, height: 900 });
        await page.goto(path);
        await expect(page.getByRole('heading', { level: 1, name: heading })).toBeVisible();
        await expect(page).toHaveScreenshot(`knowledge-${name}-${theme}-${width}.png`, { fullPage: true });
      });
    }
  }
}

// ADR 0038 decisions 1 and 2: sync only on demand, a Confluence root page, and Replace value on the alert.
const widths = [1440, 1280, 1024, 768, 390] as const;

for (const scheme of ['light', 'dark'] as const) {
  for (const width of widths) {
    test.describe(`source options at ${width} px in ${scheme} mode`, () => {
      test.beforeEach(async ({ page }, testInfo) => {
        test.skip(testInfo.project.name !== 'desktop-1440', 'the matrix sets its own viewports');
        await page.setViewportSize({ width, height: 900 });
        await page.emulateMedia({ colorScheme: scheme });
      });

      test('Add a source offers Only when someone presses Sync, and Confluence a Root page', async ({ page }) => {
        await page.goto('/o/acme/knowledge/new?kind=confluence');
        await expect(page.getByLabel('Root page (optional)')).toBeVisible();
        const every = page.getByLabel('Sync every');
        await every.selectOption({ label: 'Only when someone presses Sync' });
        await expect(every).toHaveValue('0');
        await page.getByLabel('Root page (optional)').fill('abc');
        await page.getByRole('button', { name: 'Add source' }).click();
        await expect(page.getByText('A page id is digits only, such as 12345.')).toBeVisible();
        await fits(page);
      });

      test('a source page keeps the on-demand choice and the root page, with no next sync', async ({ page }) => {
        await page.goto('/o/acme/knowledge/support-macros');
        await expect(page.getByLabel('Sync every')).toHaveValue('0');
        await expect(page.getByText(/Next /)).toHaveCount(0);
        await page.goto('/o/acme/knowledge/product-specs');
        await expect(page.getByLabel('Root page (optional)')).toHaveValue('12345');
        await fits(page);
      });

      test('a failing source with a secret offers Replace value before Open source', async ({ page }) => {
        await page.goto('/o/acme/knowledge');
        const alert = page.getByRole('alert').filter({ hasText: 'product-specs' });
        await expect(alert.getByRole('link')).toHaveText(['Replace value', 'Open source']);
        await expect(page.getByRole('alert').filter({ hasText: 'dashboards' }).getByRole('link')).toHaveText([
          'Open source',
        ]);
        await alert.getByRole('link', { name: 'Replace value' }).click();
        await expect(page).toHaveURL(/\/o\/acme\/secrets\/confluence-token/);
        await expect(page.getByRole('heading', { name: 'confluence-token' })).toBeVisible();
      });
    });
  }
}
