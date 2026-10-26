// A router harness for component tests: mounts one screen under the /o/$org/f/$factory/board
// route with the route context the screens read (the organization and the factory), and with the
// queries it reads seeded, so no request leaves the test. Used by tests only.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
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
import { boardQuery, builderMetaQuery, factoriesQuery } from '../api/queries';
import type { Board, BuilderMeta, Factory, OrgSummary } from '../api/types';

/** jsdom has no CSS.escape, scrollIntoView or scrollTo, which the board's picker and the router call. */
function polyfillLayout() {
  if (typeof globalThis.CSS === 'undefined') {
    Object.defineProperty(globalThis, 'CSS', { value: { escape: (value: string) => value } });
  }
  if (!Element.prototype.scrollIntoView) Element.prototype.scrollIntoView = () => {};
  window.scrollTo = () => {};
}

/**
 * Renders `page` at /o/<org>/f/<factory>/<section>?station=…&<search>, with the board seeded as its
 * query data. The section is the board by default; the items list renders under `items`.
 * Returns what render returns (unmount, and the rest of Testing Library's queries) and the router,
 * whose location a test reads after a control changed the URL.
 */
export function renderBoardPage({
  page,
  org,
  factory,
  factories = [factory],
  board,
  builderMeta,
  station,
  search,
  section = 'board',
}: {
  page: () => ReactNode;
  org: OrgSummary;
  factory: Factory;
  /** The organization's factories, for rows about another factory; `factory` alone by default. */
  factories?: Factory[];
  /** The board the page reads; left out for a page that does not read it. */
  board?: Board;
  /** The install's models the page labels runs with; left out for a page that names no model. */
  builderMeta?: BuilderMeta;
  station?: string;
  /** Further search params the page is at (a filter's choice, a grouping), beside `station`. */
  search?: Record<string, string>;
  /** The factory's section the page is at: its route path. */
  section?: 'board' | 'items';
}) {
  polyfillLayout();
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  queryClient.setQueryData(factoriesQuery(org.id).queryKey, factories);
  if (board) queryClient.setQueryData(boardQuery(org.id, factory.id).queryKey, board);
  if (builderMeta) queryClient.setQueryData(builderMetaQuery(org.id).queryKey, builderMeta);

  const rootRoute = createRootRoute();
  const orgRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/o/$org',
    beforeLoad: () => ({ org }),
    component: Outlet,
  });
  const factoryRoute = createRoute({
    getParentRoute: () => orgRoute,
    path: '/f/$factory',
    beforeLoad: () => ({ factory }),
    component: Outlet,
  });
  const boardRoute = createRoute({
    getParentRoute: () => factoryRoute,
    path: 'board',
    component: section === 'board' ? page : () => null,
  });
  const itemsRoute = createRoute({
    getParentRoute: () => factoryRoute,
    path: 'items/',
    component: section === 'items' ? page : () => null,
  });
  const itemRoute = createRoute({ getParentRoute: () => factoryRoute, path: 'items/$ref', component: () => null });
  const routeTree = rootRoute.addChildren([
    orgRoute.addChildren([factoryRoute.addChildren([boardRoute, itemsRoute, itemRoute])]),
  ]);

  const params = new URLSearchParams({ ...(station ? { station } : {}), ...search });
  const query = params.size > 0 ? `?${params}` : '';
  const at = section === 'items' ? 'items/' : 'board';
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [`/o/${org.slug}/f/${factory.name}/${at}${query}`] }),
  });

  return {
    ...render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
    router,
  };
}
