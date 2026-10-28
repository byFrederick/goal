// A router harness for the Connections screens: mounts the list or the add page under the
// organization's routes (the /o/$org layout and its _org group, as the app has them), with the org's
// grants and the queries the page reads seeded, so no request leaves the test. Used by tests only.
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
import { factoriesQuery, orgQuery } from '../api/queries';
import type { Connection, ConnectionKind, Org, OrgSummary } from '../api/types';
import { TooltipProvider } from '../ui/tooltip';

/** A query's key and the data the page reads from it, so the page's first render has it. */
export type Seed = readonly [key: QueryKey, data: unknown];

/**
 * Renders the Connections list (`page` 'list') or the Add a connection page (`page` 'new') at its URL,
 * under /o/<org>/_org/connections. `can` is the caller's organization permissions. `search` is the
 * URL's search params. Returns what render returns, and the router, whose location a test reads.
 */
export function renderConnectionsRoute({
  page,
  org,
  can = [],
  seed = [],
  search,
  components,
}: {
  page: 'list' | 'new';
  org: OrgSummary;
  can?: string[];
  seed?: readonly Seed[];
  search?: Record<string, string> | undefined;
  /** The pages the routes mount: the list's and the add page's, and the connection page a create opens. */
  components: { list: () => ReactNode; add: () => ReactNode; connection: () => ReactNode };
}) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  const grants: Org = { ...org, can, archived_at: null } as unknown as Org;
  queryClient.setQueryData(orgQuery(org.id).queryKey, grants);
  queryClient.setQueryData(factoriesQuery(org.id).queryKey, []);
  for (const [key, data] of seed) queryClient.setQueryData(key, data);

  const rootRoute = createRootRoute();
  const orgRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/o/$org',
    beforeLoad: () => ({ org }),
    component: Outlet,
  });
  const orgPages = createRoute({ getParentRoute: () => orgRoute, id: '_org', component: Outlet });
  const connections = createRoute({ getParentRoute: () => orgPages, path: 'connections', component: Outlet });
  const list = createRoute({ getParentRoute: () => connections, path: '/', component: components.list });
  const add = createRoute({ getParentRoute: () => connections, path: 'new', component: components.add });
  const connection = createRoute({
    getParentRoute: () => connections,
    path: '$connection',
    component: components.connection,
  });
  const routeTree = rootRoute.addChildren([
    orgRoute.addChildren([orgPages.addChildren([connections.addChildren([list, add, connection])])]),
  ]);

  const query = search && Object.keys(search).length > 0 ? `?${new URLSearchParams(search)}` : '';
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({
      initialEntries: [`/o/${org.slug}/connections${page === 'new' ? '/new' : ''}${query}`],
    }),
  });

  return {
    ...render(
      <QueryClientProvider client={queryClient}>
        <TooltipProvider>
          <RouterProvider router={router} />
        </TooltipProvider>
      </QueryClientProvider>,
    ),
    router,
    queryClient,
  };
}

/** A kind as the API answers it, with the fields a page reads. */
export function kindFixture(over: Partial<ConnectionKind> & Pick<ConnectionKind, 'name' | 'title'>): ConnectionKind {
  return {
    description: '',
    platform: true,
    fields: [],
    modes: ['env'],
    model: false,
    testable: true,
    env: [],
    ...over,
  } as ConnectionKind;
}

/** A connection as the API answers it: no secret value, only the names set. */
export function connectionFixture(over: Partial<Connection> & Pick<Connection, 'name' | 'kind'>): Connection {
  return {
    id: `id-${over.name}`,
    description: '',
    fields: {},
    secrets_set: [],
    status: 'untested',
    used_by: { factories: [], knowledge_sources: [] },
    last_used_at: null,
    last_test: null,
    tools: [],
    waiting_approvals: 0,
    created_by: { kind: 'user', id: 'user-1', name: 'Freddy Cruz' },
    created_at: '2026-10-01T00:00:00Z',
    updated_by: { kind: 'user', id: 'user-1', name: 'Freddy Cruz' },
    updated_at: '2026-10-01T00:00:00Z',
    ...over,
  } as Connection;
}
