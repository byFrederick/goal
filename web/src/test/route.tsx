// A router harness for component tests: mounts one screen under a factory's route with the route
// context the screens read (the organization and the factory), and with the queries it reads seeded,
// so no request leaves the test. Used by tests only.
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
import { boardQuery, builderMetaQuery, factoriesQuery, itemQuery, partsQuery } from '../api/queries';
import type { AssemblyParts, Board, BuilderMeta, Factory, Item, OrgSummary } from '../api/types';

/** jsdom has no CSS.escape, scrollIntoView or scrollTo, which the board's picker and the router call. */
function polyfillLayout() {
  if (typeof globalThis.CSS === 'undefined') {
    Object.defineProperty(globalThis, 'CSS', { value: { escape: (value: string) => value } });
  }
  if (!Element.prototype.scrollIntoView) Element.prototype.scrollIntoView = () => {};
  window.scrollTo = () => {};
}

/** A query's key and the data the page reads from it, so the page's first render has it. */
export type Seed = readonly [key: QueryKey, data: unknown];

/** The item route's path under the factory; a page on it is mounted at the item's own URL. */
const itemPath = 'items/$ref';

/**
 * Renders `page` at /o/<org>/f/<factory>/<path> (`path` is the factory route's child: 'board',
 * 'items/', 'items/$ref', or '/' for the Floor), with `seed` as the query data and `search` as further
 * search params beside `station`. `location` is the URL to start at when it is not the path itself (an
 * item's page, for example). Returns what render returns, and the router, whose location a test reads
 * after a control changed the URL.
 */
export function renderFactoryPage({
  page,
  path,
  location,
  org,
  factory,
  factories = [factory],
  seed = [],
  station,
  search,
}: {
  page: () => ReactNode;
  /** The page's route under the factory: 'board', 'items/', 'items/$ref' or '/' (the Floor). */
  path: string;
  /** The URL to render, when the path's parameters are not in it. */
  location?: string;
  org: OrgSummary;
  factory: Factory;
  /** The organization's factories, for rows about another factory; `factory` alone by default. */
  factories?: Factory[];
  seed?: readonly Seed[];
  station?: string | undefined;
  /** Further search params the page is at (a filter's choice, a grouping), beside `station`. */
  search?: Record<string, string> | undefined;
}) {
  polyfillLayout();
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  queryClient.setQueryData(factoriesQuery(org.id).queryKey, factories);
  for (const [key, data] of seed) queryClient.setQueryData(key, data);

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
  const pageRoute = createRoute({ getParentRoute: () => factoryRoute, path, component: page });
  // The item route exists for links between pages; a page on it is the item route itself.
  const itemRoute = createRoute({ getParentRoute: () => factoryRoute, path: itemPath, component: () => null });
  const children = path === itemPath ? [pageRoute] : [pageRoute, itemRoute];
  const routeTree = rootRoute.addChildren([orgRoute.addChildren([factoryRoute.addChildren(children)])]);

  const params = new URLSearchParams({ ...(station ? { station } : {}), ...search });
  const query = params.size > 0 ? `?${params}` : '';
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({
      initialEntries: [`/o/${org.slug}/f/${factory.name}/${location ?? (path === '/' ? '' : path)}${query}`],
    }),
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

/**
 * Renders a Board screen at /o/<org>/f/<factory>/board?station=…, with the board seeded as its query
 * data. Returns what render returns (unmount, and the rest of Testing Library's queries).
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
}: {
  page: () => ReactNode;
  org: OrgSummary;
  factory: Factory;
  factories?: Factory[];
  /** The board the page reads; left out for a page that does not read it. */
  board?: Board;
  /** The install's models the page labels runs with; left out for a page that names no model. */
  builderMeta?: BuilderMeta;
  station?: string;
  /** Further search params the page is at (a filter's choice, a grouping), beside `station`. */
  search?: Record<string, string> | undefined;
}) {
  const seed: Seed[] = [];
  if (board) seed.push([boardQuery(org.id, factory.id).queryKey, board]);
  if (builderMeta) seed.push([builderMetaQuery(org.id).queryKey, builderMeta]);
  return renderFactoryPage({ page, path: 'board', org, factory, factories, seed, station, search });
}

/**
 * Renders `page` at /o/<org>/f/<factory>/items/<ref>, with the item and its assembly's parts seeded
 * as their query data (`parts` left out for an item that is not an assembly) and the board, when
 * the page reads it.
 */
export function renderItemPage({
  page,
  org,
  factory,
  item,
  parts,
  board,
}: {
  page: () => ReactNode;
  org: OrgSummary;
  factory: Factory;
  item: Item;
  parts?: AssemblyParts;
  board?: Board;
}) {
  const seed: Seed[] = [[itemQuery(org.id, item.ref).queryKey, item]];
  if (parts) seed.push([partsQuery(org.id, item.ref).queryKey, parts]);
  if (board) seed.push([boardQuery(org.id, factory.id).queryKey, board]);
  const number = item.ref.slice(item.ref.lastIndexOf('#') + 1);
  return renderFactoryPage({
    page,
    path: itemPath,
    location: `items/${number}`,
    org,
    factory,
    seed,
  });
}
