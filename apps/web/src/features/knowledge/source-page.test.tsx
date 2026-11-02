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
import { cleanup, render, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { knowledgeSourceQuery, orgQuery } from '../../api/queries';
import type { KnowledgeSource, Org, OrgSummary } from '../../api/types';
import { TooltipProvider } from '../../ui/tooltip';
import { SourcePage } from './source-page';

// The top bar is the app's shell: here the page's actions render in a region named Top bar, where the page puts them.
vi.mock('../../app/top-bar', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../app/top-bar')>()),
  TopBarSlot: ({ actions }: { actions?: React.ReactNode }) => <section aria-label="Top bar">{actions}</section>,
}));

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const summary: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'Ac', factory_count: 1, role: 'owner' };

const synced = {
  id: 'src-1',
  name: 'goal-docs',
  title: 'goal docs',
  kind: 'synced',
  connection: 'github',
  status: 'idle',
  created_by: { kind: 'user', id: 'u1', name: 'Ada' },
  created_at: '2026-10-01T00:00:00Z',
  updated_at: '2026-10-01T00:00:00Z',
  last_synced_at: '2026-10-27T14:00:00Z',
  next_sync_at: '2026-10-27T15:00:00Z',
  config: {},
  sync_every_seconds: 3600,
  documents: 0,
  used_by: { factories: [] },
} as unknown as KnowledgeSource;

function renderSource(source: KnowledgeSource, can: string[]) {
  // What the page asks besides the source never answers; the test looks only at the top bar.
  vi.spyOn(globalThis, 'fetch').mockImplementation(() => new Promise(() => {}));
  const org = { ...summary, created_at: '2026-09-01T00:00:00Z', can } as Org;
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(orgQuery(org.id).queryKey, org);
  client.setQueryData(knowledgeSourceQuery(org.id, source.name).queryKey, source);

  const rootRoute = createRootRoute();
  const orgRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/o/$org',
    beforeLoad: () => ({ org: summary }),
    component: Outlet,
  });
  const layout = createRoute({ getParentRoute: () => orgRoute, id: '_org', component: Outlet });
  const knowledge = createRoute({ getParentRoute: () => layout, path: 'knowledge', component: Outlet });
  const page = createRoute({ getParentRoute: () => knowledge, path: '$source', component: SourcePage });
  const router = createRouter({
    routeTree: rootRoute.addChildren([orgRoute.addChildren([layout.addChildren([knowledge.addChildren([page])])])]),
    history: createMemoryHistory({ initialEntries: [`/o/acme/knowledge/${source.name}`] }),
  });
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <RouterProvider router={router} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

describe('the knowledge source page', () => {
  it('puts Sync now in the top bar, once, for a manager of a synced source', async () => {
    renderSource(synced, ['knowledge:manage']);

    const bar = await screen.findByRole('region', { name: 'Top bar' });
    expect(within(bar).getByRole('button', { name: /^Sync now/ })).toBeTruthy();
    expect(screen.getAllByRole('button', { name: /^Sync now/ })).toHaveLength(1);
  });

  it('leaves the top bar empty without knowledge:manage', async () => {
    renderSource(synced, []);

    const bar = await screen.findByRole('region', { name: 'Top bar' });
    expect(within(bar).queryByRole('button')).toBeNull();
    expect(screen.queryByRole('button', { name: /^Sync now/ })).toBeNull();
  });
});
