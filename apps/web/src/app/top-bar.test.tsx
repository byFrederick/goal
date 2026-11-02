// @vitest-environment jsdom
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router';
import { cleanup, render, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { factoriesQuery } from '../api/queries';
import type { Factory, OrgSummary } from '../api/types';
import { routeTree } from '../routeTree.gen';
import { ShellFrame } from './app-shell';
import { orgRoutePages, pageLabel, TopBarSlot } from './top-bar';

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'Ac', factory_count: 1, role: 'owner' };

const software: Factory = {
  id: 'software-id',
  org_id: 'org-1',
  name: 'software',
  title: 'Software',
  work_types: [],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'editor',
  can: [],
} as Factory;

/** The item page's query: pending while `item` is undefined, so the page has no crumb yet. */
const itemQueryKey = ['test-item'];

/** An item page that sets its crumb when its data arrives, and its actions at once. */
function ItemStub() {
  const { data } = useQuery({
    queryKey: itemQueryKey,
    queryFn: () => new Promise<{ number: number }>(() => {}),
  });
  return (
    <>
      <TopBarSlot
        crumb={data ? `#${data.number}` : undefined}
        actions={
          <>
            <button type="button">Share</button>
            <button type="button">Publish</button>
          </>
        }
      />
      <p>Item body</p>
    </>
  );
}

/** The shell as the routes draw it: the organization's, the factory's, and the items' pages. */
function renderBar({ at, item }: { at: string; item?: { number: number } }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(factoriesQuery(org.id).queryKey, [software]);
  if (item) client.setQueryData(itemQueryKey, item);

  const rootRoute = createRootRoute();
  const orgRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/o/$org',
    beforeLoad: () => ({ org }),
    component: Outlet,
  });
  const orgPages = createRoute({
    getParentRoute: () => orgRoute,
    id: '_org',
    component: () => (
      <ShellFrame>
        <Outlet />
      </ShellFrame>
    ),
  });
  const home = createRoute({ getParentRoute: () => orgPages, path: '/', component: () => <p>Home page</p> });
  const page = (path: string) =>
    createRoute({ getParentRoute: () => orgPages, path, component: () => <p>{path} page</p> });
  const connections = createRoute({ getParentRoute: () => orgPages, path: 'connections', component: Outlet });
  const connectionsIndex = createRoute({
    getParentRoute: () => connections,
    path: '/',
    component: () => <p>Connections page</p>,
  });
  const connectionRoute = createRoute({
    getParentRoute: () => connections,
    path: '$connection',
    component: () => <TopBarSlot crumb="github" />,
  });
  const factoryRoute = createRoute({
    getParentRoute: () => orgRoute,
    path: '/f/$factory',
    beforeLoad: () => ({ factory: software }),
    component: () => (
      <ShellFrame>
        <Outlet />
      </ShellFrame>
    ),
  });
  const floor = createRoute({ getParentRoute: () => factoryRoute, path: '/', component: () => <p>Floor page</p> });
  const items = createRoute({ getParentRoute: () => factoryRoute, path: 'items', component: Outlet });
  const itemsIndex = createRoute({ getParentRoute: () => items, path: '/', component: () => <p>Items page</p> });
  const itemRoute = createRoute({ getParentRoute: () => items, path: '$ref', component: ItemStub });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      orgRoute.addChildren([
        orgPages.addChildren([
          home,
          page('members'),
          page('needs-you'),
          page('spend'),
          connections.addChildren([connectionsIndex, connectionRoute]),
        ]),
        factoryRoute.addChildren([floor, items.addChildren([itemsIndex, itemRoute])]),
      ]),
    ]),
    history: createMemoryHistory({ initialEntries: [at] }),
  });
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

afterEach(cleanup);

/** Every route id of the generated route tree (FileRoutesById's keys). */
function collectIds(): string[] {
  return Object.keys(createRouter({ routeTree, context: { queryClient: new QueryClient() } }).routesById);
}

describe('TopBar', () => {
  it('shows Software / Items / #12 on a factory item page', async () => {
    renderBar({ at: '/o/acme/f/software/items/12', item: { number: 12 } });
    const nav = await screen.findByRole('navigation', { name: 'Breadcrumb' });
    expect(within(nav).getByRole('link', { name: 'Software' }).getAttribute('href')).toBe('/o/acme/f/software');
    expect(within(nav).getByRole('link', { name: 'Items' }).getAttribute('href')).toBe('/o/acme/f/software/items');
    expect(within(nav).getByText('#12').getAttribute('aria-current')).toBe('page');
    expect(within(nav).getAllByRole('listitem')).toHaveLength(3);
  });

  it('falls back to the page name while the item loads', async () => {
    renderBar({ at: '/o/acme/f/software/items/12' });
    const nav = await screen.findByRole('navigation', { name: 'Breadcrumb' });
    expect(within(nav).getByText('Items').getAttribute('aria-current')).toBe('page');
    expect(within(nav).queryByText(/^#/)).toBeNull();
  });

  it("renders the page's actions with the primary one last", async () => {
    renderBar({ at: '/o/acme/f/software/items/12' });
    const header = await screen.findByRole('banner');
    const buttons = within(header)
      .getAllByRole('button')
      .map((b) => b.textContent);
    expect(buttons).toEqual(['Share', 'Publish']);
  });

  it('shows the organization and the page name on an organization page', async () => {
    renderBar({ at: '/o/acme/' });
    const nav = await screen.findByRole('navigation', { name: 'Breadcrumb' });
    expect(within(nav).getByText('Home').getAttribute('aria-current')).toBe('page');
    expect(within(nav).getByRole('link', { name: 'Acme' }).getAttribute('href')).toBe('/o/acme');
    expect(within(nav).getAllByRole('listitem')).toHaveLength(2);
  });

  it.each([
    ['members', 'Members and roles'],
    ['needs-you', 'Needs you'],
    ['spend', 'Spend and billing'],
    ['connections', 'Connections'],
  ])('reads Acme > page on /%s', async (path, label) => {
    renderBar({ at: `/o/acme/${path}` });
    const nav = await screen.findByRole('navigation', { name: 'Breadcrumb' });
    expect(within(nav).getByRole('link', { name: 'Acme' }).getAttribute('href')).toBe('/o/acme');
    expect(within(nav).getByText(label).getAttribute('aria-current')).toBe('page');
    expect(within(nav).getAllByRole('listitem')).toHaveLength(2);
  });

  it('reads Acme > Connections > name on a connection page', async () => {
    renderBar({ at: '/o/acme/connections/github' });
    const nav = await screen.findByRole('navigation', { name: 'Breadcrumb' });
    expect(within(nav).getByRole('link', { name: 'Acme' }).getAttribute('href')).toBe('/o/acme');
    expect(within(nav).getByRole('link', { name: 'Connections' }).getAttribute('href')).toBe('/o/acme/connections');
    expect(within(nav).getByText('github').getAttribute('aria-current')).toBe('page');
    expect(within(nav).getAllByRole('listitem')).toHaveLength(3);
  });

  it('names a page for every shell route', () => {
    const layouts = new Set(['/o/$org/_org', '/o/$org/f/$factory', '/o/$org/f/$factory/definition']);
    const ids = collectIds();
    const unnamed = ids.filter(
      (id) =>
        (id.startsWith('/o/$org/_org/') || id.startsWith('/o/$org/f/$factory/')) &&
        !layouts.has(id) &&
        pageLabel(id) === undefined,
    );
    expect(ids.length).toBeGreaterThan(20);
    expect(unnamed).toEqual([]);
    expect(Object.keys(orgRoutePages)).toContain('/o/$org/_org/spend');
  });
});
