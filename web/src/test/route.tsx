// A router harness for component tests: mounts one screen under the /o/$org/f/$factory routes with
// the route context the screens read (the organization and the factory), and with the queries it
// reads seeded, so no request leaves the test. Used by tests only.
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

interface Seed {
  org: OrgSummary;
  factory: Factory;
  /** The organization's factories, for rows about another factory; `factory` alone by default. */
  factories?: Factory[];
  /** The board the page reads; left out for a page that does not read it. */
  board?: Board;
  /** The install's models the page labels runs with; left out for a page that names no model. */
  builderMeta?: BuilderMeta;
}

/**
 * Mounts the screen under the organization and factory routes, over a seeded query client. The
 * board and item routes both exist (links between them resolve); `pages` fills the one under test.
 */
function mount(
  { org, factory, factories = [factory], board, builderMeta }: Seed,
  queryClient: QueryClient,
  pages: { board?: () => ReactNode; item?: () => ReactNode },
  url: string,
) {
  polyfillLayout();
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
    component: pages.board ?? (() => null),
  });
  const itemRoute = createRoute({
    getParentRoute: () => factoryRoute,
    path: 'items/$ref',
    component: pages.item ?? (() => null),
  });
  const routeTree = rootRoute.addChildren([orgRoute.addChildren([factoryRoute.addChildren([boardRoute, itemRoute])])]);

  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: [url] }) });

  return render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

function newQueryClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
}

/**
 * Renders `page` at /o/<org>/f/<factory>/board?station=…, with the board seeded as its query data.
 * Returns what render returns (unmount, and the rest of Testing Library's queries).
 */
export function renderBoardPage({
  page,
  station,
  ...seed
}: Seed & {
  page: () => ReactNode;
  station?: string;
}) {
  const query = station ? `?station=${encodeURIComponent(station)}` : '';
  return mount(seed, newQueryClient(), { board: page }, `/o/${seed.org.slug}/f/${seed.factory.name}/board${query}`);
}

/**
 * Renders `page` at /o/<org>/f/<factory>/items/<ref>, with the item and its assembly's parts seeded
 * as their query data (`parts` left out for an item that is not an assembly).
 */
export function renderItemPage({
  page,
  item,
  parts,
  ...seed
}: Seed & {
  page: () => ReactNode;
  item: Item;
  parts?: AssemblyParts;
}) {
  const queryClient = newQueryClient();
  queryClient.setQueryData(itemQuery(seed.org.id, item.ref).queryKey, item);
  if (parts) queryClient.setQueryData(partsQuery(seed.org.id, item.ref).queryKey, parts);
  const number = item.ref.slice(item.ref.lastIndexOf('#') + 1);
  return mount(seed, queryClient, { item: page }, `/o/${seed.org.slug}/f/${seed.factory.name}/items/${number}`);
}
