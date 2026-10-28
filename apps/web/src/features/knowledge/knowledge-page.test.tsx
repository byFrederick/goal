// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  factoriesQuery,
  knowledgeDocumentsQuery,
  knowledgeDocumentUsesQuery,
  knowledgeSearchQuery,
  knowledgeSourcesQuery,
  knowledgeUsageQuery,
  orgQuery,
} from '../../api/queries';
import type {
  Factory,
  KnowledgeDocument,
  KnowledgeDocumentUsage,
  KnowledgeDocumentUse,
  KnowledgeHit,
  KnowledgeSource,
  Org,
  OrgSummary,
} from '../../api/types';
import { TooltipProvider } from '../../ui/tooltip';
import { KnowledgePage } from './knowledge-page';

// goal-docs' usage is answered by a load the test sets: pending never answers, error fails.
const usageLoad = vi.hoisted(() => ({ run: (): Promise<unknown> => Promise.resolve(null) }));
vi.mock('../../api/queries', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../api/queries')>();
  return {
    ...actual,
    knowledgeUsageQuery: (org: string, source: string, days?: number) => ({
      ...actual.knowledgeUsageQuery(org, source, days),
      queryFn: () => usageLoad.run(),
    }),
  };
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const summary: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'Ac', factory_count: 2, role: 'owner' };

// sre may add items, so the card's "Make an item to fix it" is offered for the sources it reads.
const sre: Factory = {
  id: 'sre-id',
  org_id: 'org-1',
  name: 'sre',
  work_types: [],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'editor',
  can: ['item:create'],
};

const responder: Factory = { ...sre, id: 'responder-id', name: 'responder' };

const base = {
  created_by: { kind: 'user', id: 'u1', name: 'Ada' },
  created_at: '2026-10-01T00:00:00Z',
  updated_at: '2026-10-01T00:00:00Z',
  config: {},
  sync_every_seconds: 3600,
  documents: 0,
  used_by: { factories: [] },
} as unknown as KnowledgeSource;

// The three sources of the picture: one synced, one failed with a retry, one synced daily.
const goalDocs: KnowledgeSource = {
  ...base,
  id: 'src-1',
  name: 'goal-docs',
  title: 'goal docs',
  kind: 'synced',
  connection: 'github',
  status: 'idle',
  last_synced_at: '2026-10-27T14:00:00Z',
  next_sync_at: '2026-10-27T15:00:00Z',
  documents: 312,
  used_by: { factories: [{ id: 'sre-id', name: 'sre' }] },
};

const dashboards: KnowledgeSource = {
  ...base,
  id: 'src-2',
  name: 'dashboards',
  title: 'Dashboards',
  kind: 'synced',
  connection: 'github',
  status: 'failed',
  last_error: 'the server refused the token',
  last_synced_at: '2026-10-27T15:00:00Z',
  next_sync_at: '2026-10-27T16:00:00Z',
  documents: 57,
  used_by: { factories: [{ id: 'responder-id', name: 'responder' }] },
};

const supportMacros: KnowledgeSource = {
  ...base,
  id: 'src-3',
  name: 'support-macros',
  title: 'Support macros',
  kind: 'synced',
  connection: 'github',
  status: 'idle',
  sync_every_seconds: 86_400,
  last_synced_at: '2026-10-27T06:00:00Z',
  next_sync_at: '2026-10-28T06:00:00Z',
  documents: 84,
  used_by: { factories: [{ id: 'responder-id', name: 'responder' }] },
};

const picture = [goalDocs, dashboards, supportMacros];

const documentOf = (id: string, title: string, extra: Partial<KnowledgeDocument> = {}): KnowledgeDocument => ({
  id,
  source: 'dashboards',
  title,
  mime: 'text/markdown',
  bytes: 120,
  status: 'indexed',
  indexed_at: '2026-10-27T15:00:00Z',
  ...extra,
});

const hit = (document: string, heading: string, snippet: string, score: number): KnowledgeHit => ({
  document,
  source: 'goal-docs',
  title: 'docs/adr/0014-factory-rules.md',
  heading,
  snippet,
  score,
});

/** The usage of goal-docs as the picture shows it (AGENTS.md 212 runs ▲61 ▼0, MASTER.md misled 6). */
const pictured: KnowledgeDocumentUsage[] = [
  {
    id: 'u-agents',
    title: 'AGENTS.md',
    source: 'goal-docs',
    runs: 212,
    agents_helped: 61,
    agents_misled: 0,
    people_helped: 0,
    people_misled: 0,
  },
  {
    id: 'u-factory',
    title: 'factory-definition.md',
    source: 'goal-docs',
    runs: 64,
    agents_helped: 22,
    agents_misled: 1,
    people_helped: 0,
    people_misled: 0,
  },
  {
    id: 'u-master',
    title: 'MASTER.md',
    source: 'goal-docs',
    runs: 41,
    agents_helped: 9,
    agents_misled: 6,
    people_helped: 0,
    people_misled: 0,
  },
  {
    id: 'u-testing',
    title: 'testing.md',
    source: 'goal-docs',
    runs: 33,
    agents_helped: 14,
    agents_misled: 0,
    people_helped: 0,
    people_misled: 0,
  },
];

const runOf = (run: string, title: string, ref: string): KnowledgeDocumentUse => ({
  run_id: run,
  item: { id: `item-${run}`, ref, title },
  factory: { id: 'sre-id', name: 'sre', title: 'sre' },
  kinds: ['search'],
  at: '2026-10-27T15:00:00Z',
});

interface Setup {
  url?: string;
  can?: string[];
  /** What the organization lists; `pending` never answers, `error` fails the request. */
  sources?: KnowledgeSource[] | 'pending' | 'error';
  /** What the factory in the URL lists (its current version); defaults to `sources`. */
  listedBy?: KnowledgeSource[];
  /** goal-docs' usage: the pictured documents by default; `pending` never answers, `error` fails the request. */
  usage?: KnowledgeDocumentUsage[] | 'pending' | 'error';
  /** The runs of the document the test opens (AGENTS.md), when it opens one. */
  uses?: KnowledgeDocumentUse[];
  /** The factories of the organization; sre may add items by default. */
  factories?: Factory[];
  hits?: KnowledgeHit[];
  q?: string;
}

function renderKnowledge({
  url = '/o/acme/knowledge/',
  can = ['knowledge:manage'],
  sources = picture,
  listedBy,
  usage = pictured,
  uses,
  factories = [sre, responder],
  hits,
  q,
}: Setup = {}) {
  const org: Org = { ...summary, created_at: '2026-09-01T00:00:00Z', can } as Org;
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(orgQuery(org.id).queryKey, org);
  client.setQueryData(factoriesQuery(org.id).queryKey, factories);
  client.setQueryData(knowledgeDocumentsQuery(org.id, 'dashboards').queryKey, {
    pages: [
      {
        documents: [
          documentOf('d1', 'panels.md'),
          documentOf('d2', 'alerts.md', { status: 'failed', error: 'it has no text' }),
        ],
      },
    ],
    pageParams: [undefined],
  });
  if (sources === 'pending') {
    vi.spyOn(globalThis, 'fetch').mockImplementation(() => new Promise(() => {}));
  } else if (sources === 'error') {
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new TypeError('Failed to fetch'));
  } else {
    client.setQueryData(knowledgeSourcesQuery(org.id).queryKey, sources);
    client.setQueryData(knowledgeSourcesQuery(org.id, { factory: sre.id }).queryKey, listedBy ?? sources);
  }
  const usageKey = knowledgeUsageQuery(org.id, 'goal-docs').queryKey;
  if (usage === 'pending') {
    usageLoad.run = () => new Promise(() => {});
  } else if (usage === 'error') {
    usageLoad.run = () => Promise.reject(new TypeError('Failed to fetch'));
  } else {
    client.setQueryData(usageKey, { source: 'goal-docs', days: 7, documents: usage });
  }
  if (uses) {
    client.setQueryData(knowledgeDocumentUsesQuery(org.id, 'u-agents').queryKey, { uses });
  }
  if (hits) client.setQueryData(knowledgeSearchQuery(org.id, { q: q ?? '' }).queryKey, hits);

  const rootRoute = createRootRoute();
  const orgRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/o/$org',
    beforeLoad: () => ({ org: summary }),
    component: Outlet,
  });
  // The pathless _org layout is the route the knowledge screens are registered under.
  const layout = createRoute({ getParentRoute: () => orgRoute, id: '_org', component: Outlet });
  const knowledge = createRoute({
    getParentRoute: () => layout,
    path: 'knowledge/',
    component: KnowledgePage,
  });
  const source = createRoute({ getParentRoute: () => knowledge, path: '$source', component: () => <p>Source page</p> });
  const added = createRoute({ getParentRoute: () => knowledge, path: 'new', component: () => <p>New source</p> });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      orgRoute.addChildren([layout.addChildren([knowledge.addChildren([source, added])])]),
    ]),
    history: createMemoryHistory({ initialEntries: [url] }),
  });

  return {
    ...render(
      <QueryClientProvider client={client}>
        <TooltipProvider>
          <RouterProvider router={router} />
        </TooltipProvider>
      </QueryClientProvider>,
    ),
    router,
  };
}

describe('the Knowledge page', () => {
  it('shows the sources, the search and the usage cards as pictured, and not the old documents or who-finds cards', async () => {
    renderKnowledge();
    expect(await screen.findByRole('heading', { level: 1, name: 'Knowledge' })).toBeTruthy();
    expect(
      screen.getByText('What agents can look up before they act: synced documents, searched on every run.'),
    ).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Add a source' }).getAttribute('href')).toBe('/o/acme/knowledge/new');

    const sources = screen.getByRole('region', { name: 'Sources' });
    expect(within(sources).getByText('3')).toBeTruthy();
    for (const column of ['Source', 'Sync', 'Read by', 'Documents']) {
      expect(within(sources).getAllByRole('columnheader', { name: column }).length).toBeGreaterThan(0);
    }
    expect(within(sources).getAllByRole('link', { name: 'goal docs' }).length).toBeGreaterThan(0);
    expect(within(sources).getAllByText('Sync failed').length).toBeGreaterThan(0);

    expect(screen.getByRole('region', { name: 'Look something up' })).toBeTruthy();
    expect(screen.getByRole('region', { name: 'Where it was used' })).toBeTruthy();
    expect(screen.queryByText('Who finds what')).toBeNull();
    expect(screen.queryByText('Try a search')).toBeNull();
  });

  it('retries a failed source from its row', async () => {
    renderKnowledge();
    const retry = await screen.findAllByRole('button', { name: 'Retry now, Dashboards' });
    expect(retry.length).toBeGreaterThan(0);
  });

  it('shows the loading state with aria-busy and no placeholder shapes', async () => {
    renderKnowledge({ sources: 'pending' });
    const busy = await screen.findByText('Loading knowledge sources');
    expect(busy.closest('[aria-busy="true"]')).not.toBeNull();
    expect(screen.queryByRole('region', { name: 'Sources' })).toBeNull();
  });

  it('shows the error with Try again', async () => {
    renderKnowledge({ sources: 'error' });
    expect(
      await screen.findByText('Couldn’t load the knowledge sources. Check your connection and try again.'),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(
      await screen.findByText('Couldn’t load the knowledge sources. Check your connection and try again.'),
    ).toBeTruthy();
  });

  it('shows the empty state with Add a source for managers, and who can add one for everyone else', async () => {
    renderKnowledge({ sources: [] });
    expect(await screen.findByText('No knowledge sources yet')).toBeTruthy();
    expect(screen.getAllByRole('link', { name: 'Add a source' }).length).toBeGreaterThan(0);
  });

  it('tells a member who adds sources, with no Add a source for them', async () => {
    renderKnowledge({ sources: [], can: [] });
    expect(await screen.findByText('Owners and admins of Acme add knowledge sources.')).toBeTruthy();
    expect(screen.queryByRole('link', { name: 'Add a source' })).toBeNull();
  });

  it('narrows the Sources card to the factory the URL names, and says so when there is no such factory', async () => {
    renderKnowledge({ url: '/o/acme/knowledge/?factory=sre', listedBy: [goalDocs] });
    const sources = await screen.findByRole('region', { name: 'Sources' });
    expect(within(sources).getByText('sre may search')).toBeTruthy();
    expect(within(sources).queryByText('Dashboards')).toBeNull();
    expect(within(sources).getByRole('link', { name: 'Show every source' })).toBeTruthy();
  });

  it('says a factory the organization does not have lists nothing, never the unfiltered sources', async () => {
    renderKnowledge({ url: '/o/acme/knowledge/?factory=nope' });
    expect(await screen.findByText('Acme has no factory named nope.')).toBeTruthy();
    expect(screen.queryByText('Dashboards')).toBeNull();
  });

  it('shows the usage card as pictured: the runs and the agents’ verdicts of each document, and the fix callout', async () => {
    renderKnowledge();
    const usage = await screen.findByRole('region', { name: 'Where it was used' });
    expect(within(usage).getByText('Runs that opened each document, last 7 days')).toBeTruthy();
    const rows = within(usage).getAllByRole('listitem');
    const counts: [string, string, string, string][] = [
      ['AGENTS.md', '212', '▲ 61', '▼ 0'],
      ['factory-definition.md', '64', '▲ 22', '▼ 1'],
      ['MASTER.md', '41', '▲ 9', '▼ 6'],
      ['testing.md', '33', '▲ 14', '▼ 0'],
    ];
    expect(rows).toHaveLength(counts.length);
    counts.forEach(([name, runs, helped, misled], i) => {
      const row = rows[i];
      if (!row) return;
      expect(within(row).getByRole('button', { name })).toBeTruthy();
      expect(within(row).getByText(runs)).toBeTruthy();
      expect(within(row).getByText(helped)).toBeTruthy();
      expect(within(row).getByText(misled)).toBeTruthy();
    });
    expect(within(usage).getByText('MASTER.md misled 6 runs.')).toBeTruthy();
    expect(within(usage).getByRole('button', { name: 'Make an item to fix it' })).toBeTruthy();
    expect(within(usage).queryByText('Not available yet')).toBeNull();
    expect(within(usage).queryByRole('button', { name: /verdict|helped|misled/i })).toBeNull();
  });

  it('shows the loading state as busy with an sr-only Loading and no placeholder shapes', async () => {
    renderKnowledge({ usage: 'pending' });
    const usage = await screen.findByRole('region', { name: 'Where it was used' });
    await waitFor(() => expect(within(usage).getByText('Loading')).toBeTruthy());
    expect(within(usage).getByText('Loading').closest('[aria-busy="true"]')).not.toBeNull();
    expect(within(usage).queryByRole('listitem')).toBeNull();
  });

  it('shows the empty state when no run used the source in the last 7 days', async () => {
    renderKnowledge({ usage: [] });
    const usage = await screen.findByRole('region', { name: 'Where it was used' });
    expect(within(usage).getByText('No runs used this knowledge in the last 7 days')).toBeTruthy();
    expect(within(usage).queryByRole('button', { name: 'Make an item to fix it' })).toBeNull();
  });

  it('shows the error with Try again when the usage cannot be read', async () => {
    renderKnowledge({ usage: 'error' });
    const usage = await screen.findByRole('region', { name: 'Where it was used' });
    expect(await within(usage).findByRole('alert')).toBeTruthy();
    expect(within(usage).getByRole('button', { name: 'Try again' })).toBeTruthy();
  });

  it('opens a document’s runs, each linked to its run and its item', async () => {
    renderKnowledge({
      uses: [runOf('run-1', 'Fix the palette drift', 'sre#101'), runOf('run-2', 'Add the usage card', 'sre#102')],
    });
    const usage = await screen.findByRole('region', { name: 'Where it was used' });
    fireEvent.click(within(usage).getByRole('button', { name: 'AGENTS.md' }));
    const list = await within(usage).findByRole('list', { name: 'Runs that used AGENTS.md' });
    const item = within(list).getByRole('link', { name: 'Fix the palette drift' });
    expect(item.getAttribute('href')).toBe('/o/acme/f/sre/items/101');
    const run = within(list).getAllByRole('link', { name: 'Run' })[0];
    expect(run?.getAttribute('href')).toBe('/o/acme/f/sre/items/101#run-run-1');
  });

  it('opens the composer with the misled document as its title, for a factory that may add items', async () => {
    renderKnowledge();
    const usage = await screen.findByRole('region', { name: 'Where it was used' });
    fireEvent.click(within(usage).getByRole('button', { name: 'Make an item to fix it' }));
    const dialog = await screen.findByRole('dialog', { name: 'New item' });
    expect((within(dialog).getByLabelText('Title') as HTMLInputElement).value).toBe('Fix MASTER.md');
  });

  it('keeps the callout but offers no fix button when no factory that uses the source may add items', async () => {
    renderKnowledge({ factories: [{ ...sre, can: [] }, responder] });
    const usage = await screen.findByRole('region', { name: 'Where it was used' });
    expect(within(usage).getByText('MASTER.md misled 6 runs.')).toBeTruthy();
    expect(within(usage).queryByRole('button', { name: 'Make an item to fix it' })).toBeNull();
  });

  it('searches every source from the URL and marks the query’s words in each snippet', async () => {
    renderKnowledge({
      url: '/o/acme/knowledge/?q=rework%20limit',
      q: 'rework limit',
      hits: [
        hit('h1', 'Factory rules', 'A review gate’s maxRework caps how many times an item goes back.', 0.91),
        hit('h2', '', 'Review records each broken rule, so rework shows which rules break most.', 0.78),
      ],
    });
    const results = await screen.findByRole('list', { name: 'Results' });
    expect(within(results).getAllByRole('listitem')).toHaveLength(2);
    expect(
      within(results)
        .getAllByRole('mark')
        .map((m) => m.textContent),
    ).toContain('rework');
    expect(within(results).getByText('0.91')).toBeTruthy();
  });

  it('says when a search finds nothing', async () => {
    renderKnowledge({ url: '/o/acme/knowledge/?q=zzz', q: 'zzz', hits: [] });
    expect(await screen.findByText(/Nothing in the knowledge sources matches “zzz”/)).toBeTruthy();
  });

  it('puts the query in the URL when the search is submitted', async () => {
    const { router } = renderKnowledge();
    const input = await screen.findByRole('searchbox', { name: 'Search knowledge' });
    fireEvent.change(input, { target: { value: 'rework limit' } });
    fireEvent.submit(input.closest('form') as HTMLFormElement);
    await waitFor(() => expect(router.state.location.search).toMatchObject({ q: 'rework limit' }));
  });
});
