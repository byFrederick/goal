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
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  attentionQuery,
  connectionsQuery,
  factoriesQuery,
  meQuery,
  orgQuery,
  orgsQuery,
  overviewQuery,
} from '../api/queries';
import type { Factory, FactoryOverviewList, Me, Org, OrgAttention, OrgSummary } from '../api/types';
import { TooltipProvider } from '../ui/tooltip';
import { AccountProvider } from './account-menu';
import { ShellFrame } from './app-shell';
import { TopBarSlot } from './top-bar';

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'Ac', factory_count: 2, role: 'owner' };

const factory = (name: string, extra: Partial<Factory> = {}): Factory =>
  ({
    id: `${name}-id`,
    org_id: 'org-1',
    name,
    work_types: [],
    stations: [],
    current: null,
    draft: null,
    paused: false,
    created_at: '2026-10-01T00:00:00Z',
    role: 'editor',
    can: ['item:create'],
    ...extra,
  }) as Factory;

const factories = [factory('software'), factory('sre')];

const attention: OrgAttention = { waiting_on_you: 0, factories: [] };
const overview: FactoryOverviewList = {
  window: '7d',
  since: '2026-10-21',
  until: '2026-10-27',
  waiting_on_you: 0,
  spend_usd: 512,
  budget_usd: 1400,
  factories: [],
};
const me = {
  kind: 'user',
  mode: 'session',
  platform_admin: false,
  user: { id: 'user-1', name: 'Freddy Cruz', email: 'freddy@example.com' },
} as Me;

/** Pretends the viewport is a phone (below 768 px); the phone shell is drawn only then. */
function stubPhone(matches: boolean) {
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches,
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  }));
}

/** The organization's pages (or a factory's) as ShellFrame draws them, with a page that sets its title and actions. */
function renderShell({ at, list = factories }: { at: string; list?: Factory[] }) {
  const grants = { ...org, created_at: '2026-09-01T00:00:00Z', can: [] } as Org;
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(orgQuery(org.id).queryKey, grants);
  client.setQueryData(factoriesQuery(org.id).queryKey, list);
  client.setQueryData(meQuery.queryKey, me);
  client.setQueryData(orgsQuery.queryKey, [grants]);
  client.setQueryData(connectionsQuery(org.id).queryKey, []);
  client.setQueryData(attentionQuery(org.id).queryKey, attention);
  client.setQueryData(overviewQuery(org.id).queryKey, overview);

  const rootRoute = createRootRoute();
  const orgRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/o/$org',
    beforeLoad: () => ({ org }),
    component: () => (
      <AccountProvider>
        <Outlet />
      </AccountProvider>
    ),
  });
  const orgPages = createRoute({
    getParentRoute: () => orgRoute,
    id: '_org',
    component: () => (
      <ShellFrame>
        <TopBarSlot actions={<button type="button">Invite</button>} />
        <Outlet />
      </ShellFrame>
    ),
  });
  const home = createRoute({ getParentRoute: () => orgPages, path: '/', component: () => <p>Factories</p> });
  const factoryRoute = createRoute({
    getParentRoute: () => orgRoute,
    path: '/f/$factory',
    beforeLoad: () => ({ factory: factories[0] }),
    component: () => (
      <ShellFrame>
        <Outlet />
      </ShellFrame>
    ),
  });
  const floor = createRoute({ getParentRoute: () => factoryRoute, path: '/', component: () => <p>Floor page</p> });
  const board = createRoute({ getParentRoute: () => factoryRoute, path: 'board', component: () => <p>Board page</p> });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      orgRoute.addChildren([orgPages.addChildren([home]), factoryRoute.addChildren([floor, board])]),
    ]),
    history: createMemoryHistory({ initialEntries: [at] }),
  });
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <RouterProvider router={router} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

/** The bottom tab bar, once the router has drawn the page. */
async function tabs() {
  return screen.findByRole('navigation', { name: 'Tabs' });
}

beforeEach(() => {
  stubPhone(true);
  localStorage.clear();
});

afterEach(() => {
  cleanup();
  // @ts-expect-error restoring the jsdom default, which has no matchMedia
  delete window.matchMedia;
});

describe('the phone top bar', () => {
  it('shows the page title and the page actions, and no factory squares', async () => {
    renderShell({ at: '/o/acme/f/software' });
    const bar = await screen.findByRole('banner');
    expect(within(bar).getByText('Floor')).toBeTruthy();
    expect(within(bar).getByRole('button', { name: 'Open menu' })).toBeTruthy();
    expect(screen.queryByRole('navigation', { name: 'Organization and factories' })).toBeNull();
  });

  it('shows the page title and actions of an organization page', async () => {
    renderShell({ at: '/o/acme' });
    const bar = await screen.findByRole('banner');
    expect(within(bar).getByText('Home')).toBeTruthy();
    expect(within(bar).getByRole('button', { name: 'Invite' })).toBeTruthy();
  });

  it('draws nothing of the phone shell at 768 px and wider', async () => {
    stubPhone(false);
    renderShell({ at: '/o/acme/f/software' });
    await screen.findByText('Floor page');
    expect(screen.queryByRole('navigation', { name: 'Tabs' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Open menu' })).toBeNull();
  });
});

describe('the drawer', () => {
  it('opens from the Goal mark and names the organization', async () => {
    renderShell({ at: '/o/acme/f/software' });
    const mark = await screen.findByRole('button', { name: 'Open menu' });
    expect(mark.getAttribute('aria-haspopup')).toBe('dialog');
    expect(mark.getAttribute('aria-expanded')).toBe('false');
    fireEvent.click(mark);
    const drawer = await screen.findByRole('dialog', { name: 'Acme' });
    expect(await within(drawer).findByRole('button', { name: /New item/ })).toBeTruthy();
    expect(within(drawer).getByRole('link', { name: 'Home' })).toBeTruthy();
  });

  it('opens from More in the tab bar', async () => {
    renderShell({ at: '/o/acme/f/software' });
    const more = within(await tabs()).getByRole('button', { name: 'More' });
    fireEvent.click(more);
    expect(await screen.findByRole('dialog', { name: 'Acme' })).toBeTruthy();
  });

  it('closes when a link in it is followed', async () => {
    renderShell({ at: '/o/acme/f/software' });
    fireEvent.click(await screen.findByRole('button', { name: 'Open menu' }));
    const drawer = await screen.findByRole('dialog', { name: 'Acme' });
    fireEvent.click(within(drawer).getByRole('link', { name: 'Board' }));
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Acme' })).toBeNull());
  });

  it('returns focus to the Goal mark when Close is pressed', async () => {
    renderShell({ at: '/o/acme/f/software' });
    const mark = await screen.findByRole('button', { name: 'Open menu' });
    act(() => mark.focus());
    fireEvent.click(mark);
    const drawer = await screen.findByRole('dialog', { name: 'Acme' });
    fireEvent.click(within(drawer).getByRole('button', { name: 'Close menu' }));
    await waitFor(() => expect(document.activeElement).toBe(mark));
  });

  it('disables Needs you with the not-available tooltip', async () => {
    renderShell({ at: '/o/acme/f/software' });
    fireEvent.click(await screen.findByRole('button', { name: 'Open menu' }));
    const drawer = await screen.findByRole('dialog', { name: 'Acme' });
    const needs = within(drawer).getByRole('button', { name: 'Needs you' });
    expect(needs.getAttribute('aria-disabled')).toBe('true');
  });
});

describe('the tab bar', () => {
  it('marks Needs you as not available', async () => {
    renderShell({ at: '/o/acme/f/software' });
    const needs = within(await tabs()).getByRole('button', { name: 'Needs you' });
    expect(needs.getAttribute('aria-disabled')).toBe('true');
  });

  it('opens the factory the page is in, and marks the current tab', async () => {
    renderShell({ at: '/o/acme/f/software' });
    const nav = await tabs();
    expect(within(nav).getByRole('link', { name: 'Floor' }).getAttribute('aria-current')).toBe('page');
    expect(within(nav).getByRole('link', { name: 'Board' }).getAttribute('href')).toBe('/o/acme/f/software/board');
    expect(within(nav).getByRole('link', { name: 'Items' }).getAttribute('href')).toBe('/o/acme/f/software/items');
  });

  it('opens the last factory visited in the organization outside a factory', async () => {
    localStorage.setItem('goal.factory.org-1', 'sre-id');
    renderShell({ at: '/o/acme' });
    const nav = await tabs();
    expect(within(nav).getByRole('link', { name: 'Board' }).getAttribute('href')).toBe('/o/acme/f/sre/board');
  });

  it('falls back to the first factory when the last one is gone', async () => {
    localStorage.setItem('goal.factory.org-1', 'archived-id');
    renderShell({ at: '/o/acme' });
    const nav = await tabs();
    expect(within(nav).getByRole('link', { name: 'Board' }).getAttribute('href')).toBe('/o/acme/f/software/board');
  });

  it('opens the Factories home when the organization has no factory', async () => {
    renderShell({ at: '/o/acme', list: [] });
    const nav = await tabs();
    expect(within(nav).getByRole('link', { name: 'Floor' }).getAttribute('href')).toBe('/o/acme');
    expect(within(nav).getByRole('link', { name: 'Items' }).getAttribute('href')).toBe('/o/acme');
  });
});
