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
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { meQuery, orgQuery } from '../api/queries';
import type { Me, Org, OrgSummary } from '../api/types';
import { TooltipProvider } from '../ui/tooltip';
import { AccountMenu, AccountProvider } from './account-menu';

const org: OrgSummary = { id: 'org-goal', slug: 'goal', name: 'Goal', mark: 'Go', factory_count: 1, role: 'owner' };

const session = {
  kind: 'user',
  mode: 'session',
  platform_admin: false,
  user: { id: 'user-1', name: 'Freddy Cruz', email: 'freddy@example.com' },
} as Me;

interface Setup {
  /** The account's query: a session, sign-in off, still loading, or failed. */
  me?: Me | 'pending' | 'error';
  /** The permissions the caller holds in the organization. */
  can?: string[];
}

/** jsdom has no matchMedia, which the theme reads to follow the system. */
function stubMatchMedia() {
  window.matchMedia = (query: string) =>
    ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }) as MediaQueryList;
}

/** The account menu at the sidebar's bottom, inside the account's provider, as the app mounts it. */
function renderAccount({ me = session, can = ['personal_token:read'] }: Setup = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  const grants = { ...org, created_at: '2026-09-01T00:00:00Z', can } as Org;
  client.setQueryData(orgQuery(org.id).queryKey, grants);
  // A request that never settles: the person is still loading.
  if (me === 'pending')
    void client.fetchQuery({ queryKey: meQuery.queryKey, queryFn: () => new Promise<Me>(() => {}) });
  else if (me === 'error') vi.spyOn(globalThis, 'fetch').mockRejectedValue(new TypeError('Failed to fetch'));
  else client.setQueryData(meQuery.queryKey, me);

  const rootRoute = createRootRoute();
  const orgRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/o/$org',
    beforeLoad: () => ({ org }),
    component: () => (
      <AccountProvider>
        <AccountMenu org={org} size="sidebar" />
        <Outlet />
      </AccountProvider>
    ),
  });
  const tokens = createRoute({ getParentRoute: () => orgRoute, path: 'api-keys', component: () => <p>Keys page</p> });
  const home = createRoute({ getParentRoute: () => orgRoute, path: '/', component: () => <p>Home page</p> });
  const account = createRoute({
    getParentRoute: () => rootRoute,
    path: '/account',
    component: () => <p>Account page</p>,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([orgRoute.addChildren([home, tokens]), account]),
    history: createMemoryHistory({ initialEntries: ['/o/goal'] }),
  });

  render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <RouterProvider router={router} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

/** Opens the account panel from the sidebar's person row and returns the panel. */
async function openPanel() {
  fireEvent.click(await screen.findByRole('button', { name: /account$/ }));
  return screen.findByRole('dialog', { name: 'Account' });
}

beforeEach(stubMatchMedia);

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe('the account menu', () => {
  it('names the person with their role pill, and has no GitHub line the session does not carry', async () => {
    renderAccount();
    const panel = await openPanel();
    expect(within(panel).getByText('Freddy Cruz')).toBeTruthy();
    expect(within(panel).getByText('Owner')).toBeTruthy();
    expect(within(panel).queryByText(/GitHub/)).toBeNull();
  });

  it('shows the theme inline with Auto, Light and Dark', async () => {
    renderAccount();
    const panel = await openPanel();
    const theme = within(panel).getByRole('group', { name: 'Theme' });
    for (const label of ['Auto', 'Light', 'Dark']) {
      expect(within(theme).getByRole('radio', { name: label })).toBeTruthy();
    }
  });

  it('lists the rows in the picture’s order, with the pages not built yet marked disabled', async () => {
    renderAccount();
    const panel = await openPanel();
    const text = panel.textContent ?? '';
    const order = [
      'Theme',
      'Profile',
      'Notifications',
      'Personal tokens',
      'Connected apps',
      'Keyboard shortcuts',
      'Sign out',
      'Sign out everywhere',
    ];
    const at = order.map((label) => text.indexOf(label));
    expect(at.every((i) => i >= 0)).toBe(true);
    expect([...at].sort((a, b) => a - b)).toEqual(at);
    for (const name of ['Notifications', 'Connected apps', 'Keyboard shortcuts']) {
      expect(
        within(panel)
          .getByRole('button', { name: new RegExp(`^${name}`) })
          .getAttribute('aria-disabled'),
      ).toBe('true');
    }
    expect(within(panel).getByRole('button', { name: /^Keyboard shortcuts/ }).textContent).toContain('?');
  });

  it('links Profile to the account page, which holds the name', async () => {
    renderAccount();
    const panel = await openPanel();
    expect((await within(panel).findByRole('link', { name: 'Profile' })).getAttribute('href')).toBe('/account');
    expect(screen.queryByRole('dialog', { name: 'Edit name' })).toBeNull();
  });

  it('links Personal tokens to its section of the account page, for someone who may read them', async () => {
    renderAccount({ can: ['personal_token:read'] });
    const panel = await openPanel();
    expect((await within(panel).findByRole('link', { name: 'Personal tokens' })).getAttribute('href')).toBe(
      '/account#personal-tokens',
    );
  });

  it('hides Personal tokens from someone who may not read them', async () => {
    renderAccount({ can: [] });
    const panel = await openPanel();
    await waitFor(() => expect(within(panel).queryByRole('link', { name: 'Personal tokens' })).toBeNull());
  });

  it('opens the sign-out-everywhere confirmation with Cancel focused', async () => {
    renderAccount();
    const panel = await openPanel();
    fireEvent.click(within(panel).getByRole('button', { name: 'Sign out everywhere' }));
    const dialog = await screen.findByRole('dialog', { name: 'Sign out of every browser and the CLI?' });
    expect(document.activeElement).toBe(within(dialog).getByRole('button', { name: 'Cancel' }));
  });

  it('renders the sign-out rows in the danger colour', async () => {
    renderAccount();
    const panel = await openPanel();
    expect(
      within(panel).getByRole('button', { name: 'Sign out everywhere' }).classList.contains('text-rework-ink'),
    ).toBe(true);
  });

  it('shows Loading while the person loads', async () => {
    renderAccount({ me: 'pending' });
    const panel = await openPanel();
    expect(within(panel).getByText('Loading')).toBeTruthy();
    expect(panel.querySelector('[aria-busy="true"]')).not.toBeNull();
  });

  it('says Goal could not be reached, and keeps the sign-out actions usable', async () => {
    renderAccount({ me: 'error' });
    const panel = await openPanel();
    expect(within(panel).getByText('Can’t reach Goal. Check your connection and try again.')).toBeTruthy();
    expect(within(panel).getByRole('button', { name: 'Sign out' }).hasAttribute('disabled')).toBe(false);
  });

  it('names the local user with no sign-out actions when sign-in is off', async () => {
    renderAccount({ me: { kind: 'user', mode: 'none', platform_admin: false } as Me });
    const panel = await openPanel();
    expect(within(panel).getByText('Local user')).toBeTruthy();
    expect(within(panel).queryByRole('button', { name: 'Sign out' })).toBeNull();
  });
});
