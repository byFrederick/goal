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
 * Renders `page` at /o/<org>/f/<factory>/board?station=…, with the board seeded as its query data.
 * Returns what render returns (unmount, and the rest of Testing Library's queries).
 */
export function renderBoardPage({
  page,
  org,
  factory,
  factories = [factory],
  board,
  builderMeta,
  station,
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
  const boardRoute = createRoute({ getParentRoute: () => factoryRoute, path: 'board', component: page });
  const itemRoute = createRoute({ getParentRoute: () => factoryRoute, path: 'items/$ref', component: () => null });
  const routeTree = rootRoute.addChildren([orgRoute.addChildren([factoryRoute.addChildren([boardRoute, itemRoute])])]);

  const query = station ? `?station=${encodeURIComponent(station)}` : '';
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [`/o/${org.slug}/f/${factory.name}/board${query}`] }),
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}
