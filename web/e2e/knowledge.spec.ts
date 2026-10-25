// Knowledge (spec 4 §10) against the mock: acme's sources (handbook and postmortems uploaded,
// handbook listed by sre; goal-docs and runbooks synced through github, runbooks' last sync
// failed), Try a search, a source's documents and indexed text, uploads with their refusals, New
// source of each kind, Delete with its in-use refusal, a member's view, and the builder's picker.
import { expect, type Page, test } from '@playwright/test';
import { status } from './editor';
import { expectAccessible, expectNoHorizontalScroll, expectToast, isPhone, orgNav, recordToasts } from './support';

test.beforeEach(async ({ page, request }) => {
  await request.post('/__mock/reset');
  await recordToasts(page);
});

async function fits(page: Page) {
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
}

/** A source's row in the table where main is wide, its card where it is not. */
const sourceRow = (page: Page, title: string) =>
  page
    .getByRole('row')
    .or(page.getByRole('list', { name: 'Knowledge sources of Acme' }).getByRole('listitem'))
    .filter({ has: page.getByRole('link', { name: title, exact: true }) });

test.describe('the Knowledge page', () => {
  test('lists each source with its kind, status, documents, sync and users; a failed sync says why', async ({
    page,
  }) => {
    await page.goto('/o/acme/knowledge');
    await expect(page).toHaveTitle('Knowledge · Acme · Goal');
    await expect(page.getByRole('heading', { level: 1, name: 'Knowledge' })).toBeVisible();
    await expect(page.getByRole('heading', { level: 2, name: '4 sources' })).toBeVisible();
    await expect(sourceRow(page, 'Handbook')).toContainText('Indexed');
    await expect(sourceRow(page, 'Handbook')).toContainText('Upload');
    await expect(sourceRow(page, 'goal docs')).toContainText('Synced');
    await expect(sourceRow(page, 'Runbooks')).toContainText('Sync failed');
    if (await page.getByRole('table').isVisible()) {
      await expect(sourceRow(page, 'Handbook')).toContainText('sre');
      await expect(sourceRow(page, 'Runbooks')).toContainText('answered 429');
      await expect(sourceRow(page, 'goal docs')).toContainText('github, acme/goal main, docs/**/*.md');
      await expect(sourceRow(page, 'goal docs')).toContainText(/Next in 1[45] min/);
      // Beside the list, the documents of the source that failed its sync, failures first.
      const docs = page.getByRole('region', { name: 'Runbooks' });
      await expect(docs.getByRole('listitem').first()).toContainText('Failed: GitHub answered 429');
    } else {
      await expect(sourceRow(page, 'Handbook')).toContainText('Used by sre');
    }
    await fits(page);
  });

  test('a factory the organization has no factory of is said, never the unfiltered list', async ({ page }) => {
    await page.goto('/o/acme/knowledge?factory=nope');
    await expect(page.getByText('Acme has no factory named nope.')).toBeVisible();
    await expect(page.getByRole('heading', { level: 2, name: '4 sources' })).toHaveCount(0);
    await page.getByRole('link', { name: 'Show every source' }).click();
    await expect(page.getByRole('heading', { level: 2, name: '4 sources' })).toBeVisible();
    await fits(page);
  });

  test('a search as a factory waits for the sources, never saying the factory lists none', async ({
    page,
    request,
  }) => {
    await request.post('/__mock/fail', {
      data: { path: '/knowledge/sources', method: 'GET', delay_ms: 2500, times: 0 },
    });
    await page.goto('/o/acme/knowledge?q=deploy&as=sre');
    await expect(page.getByText('Loading the knowledge sources…')).toBeVisible();
    await expect(page.getByText(/lists no knowledge source/)).toHaveCount(0);
    await expect(page.getByRole('list', { name: 'Results' }).getByRole('listitem')).toHaveCount(1, {
      timeout: 10_000,
    });
  });

  test('Sync now makes a source due, and the stream brings its new status', async ({ page }) => {
    await page.goto('/o/acme/knowledge');
    await expect(sourceRow(page, 'Runbooks')).toContainText('Sync failed');
    await sourceRow(page, 'Runbooks').getByRole('button', { name: 'Sync now, Runbooks' }).click();
    await expectToast(page, 'Runbooks syncs now');
    await expect(sourceRow(page, 'Runbooks')).not.toContainText('Sync failed');
    await expect(sourceRow(page, 'Runbooks')).toContainText('Synced');
  });

  test('tries a search as a factory: ranked snippets with their words marked and links', async ({ page }) => {
    await page.goto('/o/acme/knowledge');
    const search = page.getByRole('search', { name: 'Try a search' });
    await search.getByRole('searchbox', { name: 'Query' }).fill('memory task');
    await search.getByRole('button', { name: 'Search' }).click();
    await expect(page).toHaveURL(/q=memory\+task/);
    const said = page.getByRole('status').filter({ hasText: /results? from/ });
    await expect(said).toHaveText('3 results from every indexed source, ranked by meaning and keywords');
    const results = page.getByRole('list', { name: 'Results' });
    await expect(results.locator('mark').first()).toHaveText(/task|memory/i);
    // A synced document links back to its source; an upload opens its indexed text.
    await expect(results.getByRole('link', { name: /ecs-task-out-of-memory\.md/ })).toHaveAttribute(
      'href',
      'https://github.com/acme/runbooks/blob/main/ecs-task-out-of-memory.md',
    );
    await results.getByRole('link', { name: /checkout workers out of memory/ }).click();
    const dialog = page.getByRole('dialog', { name: /checkout workers out of memory/ });
    await expect(dialog).toContainText('Root cause: the image resize step');
    await expect(dialog.getByRole('link', { name: 'Download original' })).toHaveAttribute(
      'href',
      /\/v1\/orgs\/[^/]+\/knowledge\/documents\/[^/]+\/content$/,
    );
    await fits(page);
    await page.keyboard.press('Escape');
    await expect(dialog).toBeHidden();
    await expect(page).toHaveURL(/\/o\/acme\/knowledge\/postmortems$/);

    // As sre: only the source sre lists.
    await page.goto('/o/acme/knowledge?q=memory&as=sre');
    await expect(page.getByRole('status').filter({ hasText: 'sre lists' })).toHaveText(
      'Nothing in the 1 indexed source sre lists matches “memory”. Try fewer or other words, or search every source.',
    );
    await page.goto('/o/acme/knowledge?q=deploy&as=sre');
    await expect(page.getByRole('list', { name: 'Results' }).getByRole('listitem')).toHaveCount(1);
    await expect(page.getByRole('combobox', { name: 'As a factory' })).toHaveValue('sre');
  });

  test("the factory sidebar's Knowledge shows what the factory may search", async ({ page }) => {
    test.skip(isPhone(page), 'the factory sidebar is the 768 px and wider navigation');
    await page.goto('/o/acme/f/sre');
    await page.getByRole('navigation', { name: 'sre factory' }).getByRole('link', { name: 'Knowledge' }).click();
    await expect(page).toHaveURL(/\/o\/acme\/knowledge\?factory=sre$/);
    await expect(page.getByRole('heading', { level: 2, name: '1 source sre may search' })).toBeVisible();
    await page.getByRole('link', { name: 'Show every source' }).click();
    await expect(page.getByRole('heading', { level: 2, name: '4 sources' })).toBeVisible();
    await orgNav(page).getByRole('link', { name: 'Knowledge' }).focus();
    await expect(orgNav(page).getByRole('link', { name: 'Knowledge' })).toHaveAttribute('href', '/o/acme/knowledge');
  });

  test("on a phone, More leads to the factory's Knowledge", async ({ page }) => {
    test.skip(!isPhone(page), 'phone navigation');
    await page.goto('/o/acme/f/sre');
    await page.getByRole('navigation', { name: 'Factory' }).getByRole('button', { name: 'More' }).click();
    await page.getByRole('dialog', { name: 'More' }).getByRole('link', { name: 'Knowledge' }).click();
    await expect(page).toHaveURL(/\/o\/acme\/knowledge\?factory=sre$/);
    await expect(page.getByRole('heading', { level: 2, name: '1 source sre may search' })).toBeVisible();
  });
});

test.describe('a source', () => {
  test('uploads by choosing files: each file its own row, a refused type said before it goes', async ({ page }) => {
    await page.goto('/o/acme/knowledge/handbook');
    await expect(page).toHaveTitle('Handbook · Knowledge · Acme · Goal');
    const upload = page.getByRole('region', { name: 'Upload' });
    await expect(upload.getByRole('button', { name: /Drop files here/ })).toHaveAccessibleDescription(
      'Text, Markdown, HTML, JSON, YAML. Up to 25 MiB each. PDF and Office files come later.',
    );
    const chooser = page.waitForEvent('filechooser');
    await upload.getByRole('button', { name: /Drop files here/ }).click();
    await (await chooser).setFiles([
      { name: 'leave.md', mimeType: 'text/markdown', buffer: Buffer.from('# Leave\n\nAsk your lead first.') },
      { name: 'deck.pdf', mimeType: 'application/pdf', buffer: Buffer.from('%PDF-1.7') },
    ]);
    const files = upload.getByRole('list', { name: 'Files' });
    await expect(files.getByRole('listitem').filter({ hasText: 'leave.md' })).toContainText(
      'Uploaded, indexing starts at once',
    );
    await expect(files.getByRole('listitem').filter({ hasText: 'deck.pdf' })).toContainText(
      'Refused: Not text, Markdown, HTML, JSON or YAML',
    );
    const documents = page.getByRole('list', { name: 'Documents of Handbook' });
    await expect(documents.getByRole('listitem').filter({ hasText: 'leave.md' })).toContainText('Indexed');
    await fits(page);
  });

  test('a file goald refuses says why on its row and the others still go', async ({ page, request }) => {
    await request.post('/__mock/fail', {
      data: {
        path: '/knowledge/sources/handbook/documents',
        method: 'POST',
        status: 413,
        code: 'upload-too-large',
        detail: '"big.md" has more than 26214400 bytes',
      },
    });
    await page.goto('/o/acme/knowledge/handbook');
    const upload = page.getByRole('region', { name: 'Upload' });
    await upload.locator('input[type=file]').setInputFiles([
      { name: 'big.md', mimeType: 'text/markdown', buffer: Buffer.from('# Big') },
      { name: 'small.md', mimeType: 'text/markdown', buffer: Buffer.from('# Small') },
    ]);
    const files = upload.getByRole('list', { name: 'Files' });
    await expect(files.getByRole('listitem').filter({ hasText: 'big.md' })).toContainText(
      'Refused: "big.md" has more than 26214400 bytes',
    );
    await expect(files.getByRole('listitem').filter({ hasText: 'small.md' })).toContainText('Uploaded');
  });

  test('a document opens its indexed text; an uploaded one downloads and deletes', async ({ page }) => {
    await page.goto('/o/acme/knowledge/handbook');
    const documents = page.getByRole('list', { name: 'Documents of Handbook' });
    await documents.getByRole('link', { name: 'deploy.md' }).click();
    const dialog = page.getByRole('dialog', { name: 'deploy.md' });
    await expect(dialog.getByRole('region', { name: 'Indexed text of deploy.md' })).toContainText(
      'Deploys go out on Tuesdays',
    );
    await page.keyboard.press('Escape');
    await documents.getByRole('button', { name: 'Actions for oncall.md' }).click();
    await page.getByRole('menuitem', { name: 'Delete…' }).click();
    await page
      .getByRole('dialog', { name: 'Delete oncall.md?' })
      .getByRole('button', { name: 'Delete document' })
      .click();
    await expectToast(page, 'Deleted oncall.md');
    await expect(documents.getByRole('link', { name: 'oncall.md' })).toHaveCount(0);
  });

  test('changes what a synced source reads and how often', async ({ page }) => {
    await page.goto('/o/acme/knowledge/goal-docs');
    const settings = page.getByRole('form', { name: 'Settings' });
    await settings.getByRole('textbox', { name: 'Paths' }).fill('docs/**/*.md, README.md');
    await settings.getByRole('combobox', { name: 'Sync every' }).selectOption({ label: '6 hours' });
    await settings.getByRole('button', { name: 'Save changes' }).click();
    await expectToast(page, 'Saved goal docs');
    await page.reload();
    await expect(page.getByRole('textbox', { name: 'Paths' })).toHaveValue('docs/**/*.md, README.md');
    await expect(page.getByRole('combobox', { name: 'Sync every' })).toHaveValue(String(6 * 3600));
    await expect(page.getByText('github, acme/goal main, docs/**/*.md, README.md')).toBeVisible();
  });

  test('a sync that ends while the settings are being edited keeps the edits', async ({ page }) => {
    await page.goto('/o/acme/knowledge/goal-docs');
    const settings = page.getByRole('form', { name: 'Settings' });
    const paths = settings.getByRole('textbox', { name: 'Paths' });
    await paths.fill('docs/**/*.md, README.md');
    await settings.getByRole('textbox', { name: 'Title' }).fill('Goal docs');
    await page.getByRole('button', { name: /Sync now/ }).click();
    await expectToast(page, 'goal docs syncs now');
    // The sync's end (goald moves updated_at) reaches the page through the stream.
    await expect(page.getByText(/^Last sync/)).not.toContainText('45 min');
    await expect(paths).toHaveValue('docs/**/*.md, README.md');
    await expect(settings.getByRole('textbox', { name: 'Title' })).toHaveValue('Goal docs');
    await settings.getByRole('button', { name: 'Save changes' }).click();
    await expectToast(page, 'Saved Goal docs');
    await expect(settings.getByRole('button', { name: 'Save changes' })).toBeDisabled();
  });

  test('Delete is refused while a factory lists the source, and needs the name typed otherwise', async ({ page }) => {
    await page.goto('/o/acme/knowledge/handbook');
    await expect(page.getByRole('region', { name: 'Used by' }).getByRole('link', { name: 'sre' })).toHaveAttribute(
      'href',
      '/o/acme/f/sre/definition/connections',
    );
    const blocked = page.getByRole('button', { name: 'Delete…' });
    await expect(blocked).toHaveAttribute('aria-disabled', 'true');
    await expect(blocked).toHaveAccessibleDescription(
      'sre may search it, so Goal refuses until its definition no longer lists it. Then you type the name to confirm.',
    );
    await fits(page);

    await page.goto('/o/acme/knowledge/postmortems');
    await expect(page.getByRole('region', { name: 'Documents' }).getByRole('list')).toBeVisible();
    // Once it is deleted, nothing asks goald for the gone source's documents (a 404 that flashed).
    let deleted = false;
    const afterDelete: string[] = [];
    page.on('request', (r) => {
      const path = new URL(r.url()).pathname;
      if (r.method() === 'DELETE' && path.endsWith('/knowledge/sources/postmortems')) deleted = true;
      else if (deleted && path.includes('/knowledge/sources/postmortems')) afterDelete.push(path);
    });
    await page.getByRole('button', { name: 'Delete…' }).click();
    const dialog = page.getByRole('dialog', { name: 'Delete Postmortems?' });
    await dialog.getByLabel(/Type postmortems to confirm/).fill('postmortems');
    await dialog.getByRole('button', { name: 'Delete source' }).click();
    await expectToast(page, 'Deleted Postmortems');
    await expect(page).toHaveURL(/\/o\/acme\/knowledge$/);
    await expect(page.getByRole('heading', { level: 2, name: '3 sources' })).toBeVisible();
    await page.waitForLoadState('networkidle');
    expect(afterDelete).toEqual([]);
  });

  test('a member reads it all and changes nothing', async ({ page, request }) => {
    await request.post('/__mock/reset', { data: { role: 'member' } });
    await page.goto('/o/acme/knowledge');
    await expect(page.getByRole('heading', { level: 2, name: '4 sources' })).toBeVisible();
    await expect(page.getByRole('link', { name: /New source/ })).toHaveCount(0);
    await expect(page.getByRole('button', { name: /Sync now/ })).toHaveCount(0);
    await page.goto('/o/acme/knowledge/handbook');
    await expect(page.getByRole('region', { name: 'Upload' })).toHaveCount(0);
    await expect(page.getByRole('textbox', { name: 'Title' })).toHaveAttribute('readonly', '');
    await expect(page.getByRole('button', { name: 'Save changes' })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Delete…' })).toHaveCount(0);
    await fits(page);
  });
});

test.describe('New source', () => {
  test('synced: a connection that can sync, what of it to read, a schedule', async ({ page }) => {
    await page.goto('/o/acme/knowledge');
    await page.getByRole('link', { name: /New source/ }).click();
    await expect(page).toHaveTitle('New source · Knowledge · Acme · Goal');
    await page.getByRole('radio', { name: /Synced/ }).check();
    await expect(page).toHaveURL(/kind=synced/);
    await expect(page.getByRole('combobox', { name: 'Connection' })).toHaveValue('github');
    // Only connections whose kind can sync: no model key, no MCP server.
    await expect(page.getByRole('combobox', { name: 'Connection' }).getByRole('option')).toHaveText([
      'github, GitHub',
      'gitlab-infra, GitLab',
    ]);
    await page.getByRole('button', { name: 'Add source' }).click();
    await expect(page.getByText('Repository is required.')).toBeVisible();
    await expect(page.getByText('A title is required.')).toBeVisible();

    await page.getByRole('textbox', { name: 'Repository' }).fill('acme/infra');
    await page.getByRole('textbox', { name: 'Title' }).fill('Infra docs');
    await expect(page.getByRole('textbox', { name: 'Name' })).toHaveValue('infra-docs');
    await page.getByRole('button', { name: 'Add source' }).click();
    await expect(page.getByText('An https URL, such as https://github.com/acme/docs.')).toBeVisible();
    await page.getByRole('textbox', { name: 'Repository' }).fill('https://github.com/acme/infra');
    await page.getByRole('textbox', { name: 'Paths' }).fill('docs/**/*.md, runbooks/**/*.md');
    await page.getByRole('combobox', { name: 'Sync every' }).selectOption({ label: '15 minutes' });
    await fits(page);
    await page.getByRole('button', { name: 'Add source' }).click();

    await expect(page).toHaveURL(/\/o\/acme\/knowledge\/infra-docs\?created=true$/);
    await expect(page.getByRole('status').filter({ hasText: 'is added' })).toHaveText(
      'Infra docs is added. Its first sync starts now; this page updates when it ends.',
    );
    await expect(page.getByText('github, acme/infra, docs/**/*.md, runbooks/**/*.md')).toBeVisible();
  });

  test('upload: files chosen here go up once the source exists', async ({ page }) => {
    await page.goto('/o/acme/knowledge/new');
    await expect(page.getByRole('radio', { name: /Upload/ })).toBeChecked();
    await page.getByRole('textbox', { name: 'Title' }).fill('Handbook');
    await expect(page.getByRole('textbox', { name: 'Name' })).toHaveValue('handbook-2');
    await page.getByRole('textbox', { name: 'Name' }).fill('handbook');
    await expect(page.getByText('Acme already has a source named handbook.')).toBeVisible();
    await page.getByRole('textbox', { name: 'Name' }).fill('team-notes');
    await page
      .locator('input[type=file]')
      .setInputFiles([
        { name: 'notes.md', mimeType: 'text/markdown', buffer: Buffer.from('# Notes\n\nStand-up at 09:30.') },
      ]);
    await expect(page.getByRole('list', { name: 'Files' })).toContainText('Ready to upload');
    await fits(page);
    await page.getByRole('button', { name: 'Add source' }).click();
    await expect(page).toHaveURL(/\/o\/acme\/knowledge\/team-notes\?created=true$/);
    await expect(
      page.getByRole('list', { name: 'Documents of Handbook' }).getByRole('link', { name: 'notes.md' }),
    ).toBeVisible();
  });

  test('live: a connection that runs an MCP server, nothing stored', async ({ page }) => {
    await page.goto('/o/acme/knowledge/new?kind=live');
    await expect(page.getByRole('combobox', { name: 'Connection' }).getByRole('option')).toHaveText([
      'github, GitHub',
      'playwright, MCP server',
    ]);
    await page.getByRole('combobox', { name: 'Connection' }).selectOption('playwright');
    await page.getByRole('textbox', { name: 'Title' }).fill('Browser');
    await page.getByRole('button', { name: 'Add source' }).click();
    await expect(page).toHaveURL(/\/o\/acme\/knowledge\/browser\?created=true$/);
    await expect(page.getByText('A live source stores nothing.', { exact: false })).toBeVisible();
    await expect(page.getByRole('button', { name: /Sync now/ })).toHaveCount(0);
  });
});

test('the builder says while the organization’s sources load, never that it has none', async ({ page, request }) => {
  await request.post('/__mock/fail', { data: { path: '/knowledge/sources', method: 'GET', delay_ms: 2500, times: 0 } });
  await page.goto('/o/acme/f/sre/definition/connections');
  const knowledge = page.getByRole('region', { name: 'Knowledge sre may search' });
  await expect(knowledge.getByText('Loading Acme’s knowledge sources…')).toBeVisible();
  await expect(knowledge.getByText(/has no knowledge sources yet/)).toHaveCount(0);
  await expect(knowledge.getByRole('checkbox', { name: /^Handbook/ })).toBeChecked({ timeout: 10_000 });
});

test('the builder lists the organization’s sources to tick for the factory', async ({ page }) => {
  await page.goto('/o/acme/f/sre/definition/connections');
  const knowledge = page.getByRole('region', { name: 'Knowledge sre may search' });
  await expect(knowledge.getByRole('checkbox', { name: /^Handbook/ })).toBeChecked();
  await expect(knowledge.getByRole('checkbox', { name: /^goal docs/ })).not.toBeChecked();
  await expect(knowledge).toContainText('Synced, github, acme/goal main, docs/**/*.md');
  await knowledge.getByRole('checkbox', { name: /^Runbooks/ }).check();
  await expect(page.getByRole('region', { name: 'What runs get' })).toContainText('knowledge__search over 2 sources');
  await expect(status(page)).toContainText('Autosaved');
  await page.reload();
  await expect(
    page.getByRole('region', { name: 'Knowledge sre may search' }).getByRole('checkbox', { name: /^Runbooks/ }),
  ).toBeChecked();
  await fits(page);
});
