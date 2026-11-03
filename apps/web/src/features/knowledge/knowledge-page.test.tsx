// @vitest-environment jsdom
import { cleanup, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { factoriesQuery, knowledgeFindQuery, knowledgeReadsQuery, knowledgeSourcesQuery } from '../../api/queries';
import type { Factory, KnowledgeReadCount } from '../../api/types';
import { KnowledgePage } from './knowledge-page';
import { renderKnowledge, sourceOf, summary } from './test-harness';

vi.mock('../../app/top-bar', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../app/top-bar')>()),
  TopBarSlot: ({ actions }: { actions?: React.ReactNode }) => <section aria-label="Top bar">{actions}</section>,
}));

beforeEach(() => {
  vi.spyOn(globalThis, 'fetch').mockImplementation(() => new Promise(() => {}));
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const factories = [
  { id: 'f-sre', name: 'SRE' },
  { id: 'f-software', name: 'Software' },
] as unknown as Factory[];

const runbooks = sourceOf({
  name: 'runbooks',
  kind: 'repository',
  config: { repository: 'https://github.com/acme/ops', include: 'docs/runbooks/**' },
  latest_snapshot: { files: 212, failed: 0, bytes: 3 * 1024 * 1024, synced_at: '2026-10-27T14:00:00Z' },
  used_by: { factories: [{ id: 'f-sre', name: 'SRE' }], readers: [] },
});
const specs = sourceOf({
  name: 'product-specs',
  kind: 'confluence',
  config: { spaces: 'PROD' },
  status: 'failed',
  last_error: 'Confluence refused the token',
  latest_snapshot: { files: 310, failed: 0, bytes: 1536 * 1024, synced_at: '2026-10-26T18:00:00Z' },
});
const reads: KnowledgeReadCount[] = [
  {
    source: 'runbooks',
    path: 'db/disk-full.md',
    factory_id: 'f-sre',
    factory: 'SRE',
    day: '2026-10-27',
    reads: 14,
    searches: 2,
  },
  {
    source: 'runbooks',
    path: 'k8s/crashloop.md',
    factory_id: 'f-sre',
    factory: 'SRE',
    day: '2026-10-27',
    reads: 7,
    searches: 0,
  },
  { source: 'product-specs', path: '.', factory_id: 'f-sre', factory: 'SRE', day: '2026-10-27', reads: 0, searches: 3 },
];

const seed = (sources = [runbooks, specs]) =>
  [
    [knowledgeSourcesQuery(summary.id).queryKey, sources],
    [knowledgeReadsQuery(summary.id).queryKey, reads],
    [factoriesQuery(summary.id).queryKey, factories],
  ] as const;

describe('the Knowledge page', () => {
  it('lists each source with its read-when line, kind, factories, sync, size and reads of the last 7 days', async () => {
    renderKnowledge({ page: KnowledgePage, route: '/', location: '/o/acme/knowledge', seed: seed() });

    const table = await screen.findByRole('table', { name: 'Knowledge sources of Acme' });
    const row = within(table).getByRole('link', { name: 'runbooks' }).closest('tr') as HTMLElement;
    expect(within(row).getByText('Read before acting on an alert.')).toBeTruthy();
    expect(within(row).getByText('Repository')).toBeTruthy();
    expect(within(row).getByText('acme/ops docs/runbooks/**')).toBeTruthy();
    expect(within(row).getByText('SRE')).toBeTruthy();
    expect(within(row).getByText('212 files')).toBeTruthy();
    expect(within(row).getByText('3.0 MiB')).toBeTruthy();
    // Reads and searches of the source together: 14 + 2 + 7.
    expect(within(row).getByText('23')).toBeTruthy();
  });

  it('raises an alert naming a failing source, with a link to open it', async () => {
    renderKnowledge({ page: KnowledgePage, route: '/', location: '/o/acme/knowledge', seed: seed() });

    const alert = (await screen.findAllByRole('alert'))[0] as HTMLElement;
    expect(alert.textContent).toContain('product-specs');
    expect(alert.textContent).toContain('Confluence refused the token');
    expect(within(alert).getByRole('link', { name: 'Open source' }).getAttribute('href')).toBe(
      '/o/acme/knowledge/product-specs',
    );
  });

  it('lists the most read files, most first, leaving out searches of a whole source', async () => {
    renderKnowledge({ page: KnowledgePage, route: '/', location: '/o/acme/knowledge', seed: seed() });

    const table = await screen.findByRole('table', { name: 'Most read files' });
    const files = within(table)
      .getAllByRole('link')
      .map((a) => a.textContent);
    expect(files).toEqual(['runbooks/db/disk-full.md', 'runbooks/k8s/crashloop.md']);
  });

  it('finds files by path or title across sources', async () => {
    const found = [
      {
        source: 'runbooks',
        path: 'db/disk-full.md',
        title: 'Disk full on a Postgres primary',
        bytes: 4200,
        version: 'v1',
      },
    ];
    renderKnowledge({
      page: KnowledgePage,
      route: '/',
      location: '/o/acme/knowledge?q=disk',
      seed: [...seed(), [knowledgeFindQuery(summary.id, 'disk').queryKey, found]],
    });

    const list = await screen.findByRole('list', { name: 'Files found' });
    expect(within(list).getByText('runbooks/db/disk-full.md')).toBeTruthy();
    expect(within(list).getByText('Disk full on a Postgres primary')).toBeTruthy();
  });

  it('offers Add a source to a manager and not to a reader', async () => {
    const manager = renderKnowledge({ page: KnowledgePage, route: '/', location: '/o/acme/knowledge', seed: seed() });
    expect(await screen.findByRole('link', { name: /Add a source/ })).toBeTruthy();
    manager.unmount();
    cleanup();

    renderKnowledge({
      page: KnowledgePage,
      route: '/',
      location: '/o/acme/knowledge',
      seed: seed(),
      can: ['knowledge:read'],
    });
    await screen.findByRole('table', { name: 'Knowledge sources of Acme' });
    expect(screen.queryByRole('link', { name: /Add a source/ })).toBeNull();
  });

  it('shows a loading state while the sources load, never a blank page', async () => {
    renderKnowledge({ page: KnowledgePage, route: '/', location: '/o/acme/knowledge', seed: [] });
    expect((await screen.findAllByText('Loading')).length).toBeGreaterThan(0);
  });
});
