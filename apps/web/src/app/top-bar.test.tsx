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

/** A secret page: it names itself in the top bar's crumb. */
function SecretStub() {
  return <TopBarSlot crumb="Deploy key" />;
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
  const connection = createRoute({
    getParentRoute: () => orgPages,
    path: 'secrets/$secret',
    component: SecretStub,
  });
  const spend = createRoute({
    getParentRoute: () => orgPages,
    path: 'spend',
    component: () => (
      <>
        <TopBarSlot actions={<button type="button">Export CSV</button>} />
        <p>Spend page</p>
      </>
    ),
  });
  const models = createRoute({
    getParentRoute: () => orgPages,
    path: 'models',
    component: () => <p>Models page</p>,
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
  const intake = createRoute({ getParentRoute: () => factoryRoute, path: 'intake', component: Outlet });
  const newTrigger = createRoute({
    getParentRoute: () => intake,
    path: 'new',
    component: () => <TopBarSlot crumb="New trigger" />,
  });
  const intakeIndex = createRoute({ getParentRoute: () => intake, path: '/', component: () => <p>Intake page</p> });
  const webhooks = createRoute({
    getParentRoute: () => intake,
    path: 'webhooks',
    component: () => <TopBarSlot crumb="Webhooks" />,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      orgRoute.addChildren([
        orgPages.addChildren([home, connection, spend, models]),
        factoryRoute.addChildren([
          floor,
          items.addChildren([itemsIndex, itemRoute]),
          intake.addChildren([intakeIndex, webhooks, newTrigger]),
        ]),
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

  it('shows Software / Intake / New trigger on the New trigger picker', async () => {
    renderBar({ at: '/o/acme/f/software/intake/new' });
    const nav = await screen.findByRole('navigation', { name: 'Breadcrumb' });
    expect(within(nav).getByRole('link', { name: 'Software' }).getAttribute('href')).toBe('/o/acme/f/software');
    expect(within(nav).getByRole('link', { name: 'Intake' }).getAttribute('href')).toBe('/o/acme/f/software/intake');
    expect(within(nav).getByText('New trigger').getAttribute('aria-current')).toBe('page');
    expect(within(nav).getAllByRole('listitem')).toHaveLength(3);
  });

  it('names the spend page and shows its actions', async () => {
    renderBar({ at: '/o/acme/spend' });
    const nav = await screen.findByRole('navigation', { name: 'Breadcrumb' });
    expect(within(nav).getByText('Spend and billing').getAttribute('aria-current')).toBe('page');
    expect(within(await screen.findByRole('banner')).getByRole('button', { name: 'Export CSV' })).toBeTruthy();
  });

  it('names the models page as the current crumb', async () => {
    renderBar({ at: '/o/acme/models' });
    const nav = await screen.findByRole('navigation', { name: 'Breadcrumb' });
    expect(within(nav).getByText('Models and policies').getAttribute('aria-current')).toBe('page');
  });

  it('starts an organization page with the organization, linked, then the current page', async () => {
    renderBar({ at: '/o/acme/' });
    const nav = await screen.findByRole('navigation', { name: 'Breadcrumb' });
    expect(within(nav).getByText('Home').getAttribute('aria-current')).toBe('page');
    expect(within(nav).getByRole('link', { name: 'Acme' }).getAttribute('href')).toBe('/o/acme');
    expect(within(nav).getAllByRole('listitem')).toHaveLength(2);
  });

  it('starts a secret page with the organization, then Secrets, then its name', async () => {
    renderBar({ at: '/o/acme/secrets/c1' });
    const nav = await screen.findByRole('navigation', { name: 'Breadcrumb' });
    expect(within(nav).getByRole('link', { name: 'Acme' }).getAttribute('href')).toBe('/o/acme');
    expect(within(nav).getByRole('link', { name: 'Secrets' }).getAttribute('href')).toBe('/o/acme/secrets');
    expect(within(nav).getByText('Deploy key').getAttribute('aria-current')).toBe('page');
    expect(within(nav).getAllByRole('listitem')).toHaveLength(3);
  });

  it('shows Software / Intake on the Intake page', async () => {
    renderBar({ at: '/o/acme/f/software/intake' });
    const nav = await screen.findByRole('navigation', { name: 'Breadcrumb' });
    expect(within(nav).getByRole('link', { name: 'Software' })).toBeTruthy();
    expect(within(nav).getByText('Intake').getAttribute('aria-current')).toBe('page');
    expect(within(nav).getAllByRole('listitem')).toHaveLength(2);
  });

  it('shows Software / Intake / Webhooks on the Webhooks page', async () => {
    renderBar({ at: '/o/acme/f/software/intake/webhooks' });
    const nav = await screen.findByRole('navigation', { name: 'Breadcrumb' });
    expect(within(nav).getByRole('link', { name: 'Intake' }).getAttribute('href')).toBe('/o/acme/f/software/intake');
    expect(within(nav).getByText('Webhooks').getAttribute('aria-current')).toBe('page');
    expect(within(nav).getAllByRole('listitem')).toHaveLength(3);
  });

  it('truncates each crumb with an ellipsis and gives it its full label as a title', async () => {
    renderBar({ at: '/o/acme/f/software/items/12', item: { number: 12 } });
    const nav = await screen.findByRole('navigation', { name: 'Breadcrumb' });
    for (const [name, text] of [
      ['Software', 'Software'],
      ['Items', 'Items'],
    ] as const) {
      const link = within(nav).getByRole('link', { name });
      expect(link.className).toContain('truncate');
      expect(link.getAttribute('title')).toBe(text);
    }
    const current = within(nav).getByText('#12');
    expect(current.className).toContain('truncate');
    expect(current.getAttribute('title')).toBe('#12');
    for (const li of within(nav).getAllByRole('listitem')) expect(li.className).toContain('min-w-0');
    expect(nav.className).toContain('min-w-0');
  });

  it('truncates the crumbs of an organization page and keeps its actions from shrinking', async () => {
    renderBar({ at: '/o/acme/secrets/c1' });
    const nav = await screen.findByRole('navigation', { name: 'Breadcrumb' });
    for (const name of ['Acme', 'Secrets']) {
      const link = within(nav).getByRole('link', { name });
      expect(link.className).toContain('truncate');
      expect(link.getAttribute('title')).toBe(name);
    }
    expect(within(nav).getByText('Deploy key').getAttribute('title')).toBe('Deploy key');
  });

  it('lets the actions wrapper shrink instead of running past the gutter', async () => {
    renderBar({ at: '/o/acme/spend' });
    const button = await within(await screen.findByRole('banner')).findByRole('button', { name: 'Export CSV' });
    expect(button.parentElement?.className).toContain('min-w-0');
    expect(button.parentElement?.className).not.toContain('flex-none');
  });

  it('shrinks the crumbs far ahead of the actions wrapper', async () => {
    renderBar({ at: '/o/acme/spend' });
    const banner = await screen.findByRole('banner');
    const nav = within(banner).getByRole('navigation', { name: 'Breadcrumb' });
    const button = await within(banner).findByRole('button', { name: 'Export CSV' });
    expect(nav.className).toContain('shrink-[999]');
    expect(button.parentElement?.className).toMatch(/(^|\s)shrink(\s|$)/);
  });
});
