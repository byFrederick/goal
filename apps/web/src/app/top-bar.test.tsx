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
import { ShellFrame } from './app-shell';
import { TopBarSlot } from './top-bar';

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
        orgPages.addChildren([home]),
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

  it('shows only the page name on an organization page', async () => {
    renderBar({ at: '/o/acme/' });
    const nav = await screen.findByRole('navigation', { name: 'Breadcrumb' });
    expect(within(nav).getByText('Home').getAttribute('aria-current')).toBe('page');
    expect(within(nav).queryByRole('link')).toBeNull();
    expect(within(nav).getAllByRole('listitem')).toHaveLength(1);
  });
});
