// A router harness for the Knowledge pages' tests: mounts a page under the organization's routes with the
// queries it reads seeded, so no request leaves the test. Used by tests only.
import { QueryClient, QueryClientProvider, type QueryKey } from '@tanstack/react-query';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router';
import { render } from '@testing-library/react';
import type { ReactNode } from 'react';
import { orgQuery } from '../../api/queries';
import type { KnowledgeSource, Org, OrgSummary } from '../../api/types';
import { TooltipProvider } from '../../ui/tooltip';

export const summary: OrgSummary = {
  id: 'org-1',
  slug: 'acme',
  name: 'Acme',
  mark: 'Ac',
  factory_count: 2,
  role: 'owner',
};

export const by = { kind: 'user', id: 'u1', name: 'Ada' } as const;

/** A source as the API answers it, with the parts a test does not look at filled in. */
export function sourceOf(patch: Partial<KnowledgeSource> & Pick<KnowledgeSource, 'name' | 'kind'>): KnowledgeSource {
  return {
    id: `id-${patch.name}`,
    title: patch.name,
    config: {},
    read_when: 'Read before acting on an alert.',
    sync_every_seconds: 3600,
    status: 'idle',
    last_synced_at: '2026-10-27T14:00:00Z',
    next_sync_at: '2026-10-27T15:00:00Z',
    documents: 0,
    latest_snapshot: null,
    used_by: { factories: [], readers: [] },
    created_by: by,
    created_at: '2026-10-01T00:00:00Z',
    updated_at: '2026-10-01T00:00:00Z',
    ...patch,
  };
}

/** Renders `page` at `path` under /o/acme/knowledge, with `seed` as the query data and `can` as the member's permissions. */
export function renderKnowledge({
  page,
  route,
  location,
  seed,
  can = ['knowledge:manage', 'knowledge:read'],
}: {
  page: () => ReactNode;
  /** The page's route under knowledge: '/', 'new' or '$source'. */
  route: '/' | 'new' | '$source';
  location: string;
  seed: readonly (readonly [QueryKey, unknown])[];
  can?: string[];
}) {
  // What the page asks besides the seed never answers.
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(orgQuery(summary.id).queryKey, { ...summary, created_at: '2026-09-01T00:00:00Z', can } as Org);
  for (const [key, data] of seed) client.setQueryData(key, data);

  const rootRoute = createRootRoute();
  const orgRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/o/$org',
    beforeLoad: () => ({ org: summary }),
    component: Outlet,
  });
  const layout = createRoute({ getParentRoute: () => orgRoute, id: '_org', component: Outlet });
  const knowledge = createRoute({ getParentRoute: () => layout, path: 'knowledge', component: Outlet });
  const children = [
    createRoute({ getParentRoute: () => knowledge, path: '/', component: route === '/' ? page : () => null }),
    createRoute({ getParentRoute: () => knowledge, path: 'new', component: route === 'new' ? page : () => null }),
    createRoute({
      getParentRoute: () => knowledge,
      path: '$source',
      component: route === '$source' ? page : () => null,
    }),
  ];
  const router = createRouter({
    routeTree: rootRoute.addChildren([orgRoute.addChildren([layout.addChildren([knowledge.addChildren(children)])])]),
    history: createMemoryHistory({ initialEntries: [location] }),
  });
  const view = render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <RouterProvider router={router} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return { ...view, router, client };
}
