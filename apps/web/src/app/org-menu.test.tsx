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
import { membersQuery, meQuery, orgQuery, orgsQuery } from '../api/queries';
import type { Me, Member, Org, OrgSummary } from '../api/types';
import { Menu, MenuTrigger } from '../ui/menu';
import { TooltipProvider } from '../ui/tooltip';
import { OrgMenuContent } from './org-menu';

const goal: OrgSummary = { id: 'org-goal', slug: 'goal', name: 'Goal', mark: 'Go', factory_count: 1, role: 'owner' };
const acme: OrgSummary = { id: 'org-acme', slug: 'acme', name: 'Acme', mark: 'Ac', factory_count: 3, role: 'owner' };

const me: Me = {
  kind: 'user',
  mode: 'session',
  platform_admin: false,
  user: { id: 'user-1', name: 'Freddy Cruz', email: 'freddy@example.com' },
} as Me;

const member = (i: number): Member => ({
  member_id: `member-${i}`,
  user: { id: `user-${i}`, name: `Person ${i}`, email: `p${i}@example.com` },
  role: 'member',
  factory_roles: [],
  joined_at: '2026-09-01T00:00:00Z',
  last_active_at: null,
  you: false,
  can_change_role: false,
  can_remove: false,
});

type Members = 'ready' | 'pending' | 'error' | 'none';

interface Setup {
  orgs?: OrgSummary[];
  /** The permissions the caller holds in the current organization (its `can`). */
  can?: string[];
  members?: Members;
  platformAdmin?: boolean;
  onCreate?: () => void;
}

/** The organization menu open under the sidebar's top row, at the current organization's URL. */
function renderMenu({
  orgs = [goal, acme],
  can = ['member:read', 'api_key:read'],
  members = 'ready',
  platformAdmin = true,
  onCreate = () => {},
}: Setup = {}) {
  const current = orgs[0] ?? goal;
  const org: Org = { ...current, created_at: '2026-09-01T00:00:00Z', can } as Org;
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(orgsQuery.queryKey, orgs);
  client.setQueryData(meQuery.queryKey, { ...me, platform_admin: platformAdmin });
  client.setQueryData(orgQuery(org.id).queryKey, org);
  if (members === 'ready')
    client.setQueryData(membersQuery(org.id).queryKey, [member(1), member(2), member(3), member(4)]);
  if (members === 'pending') vi.spyOn(globalThis, 'fetch').mockImplementation(() => new Promise(() => {}));
  if (members === 'error') vi.spyOn(globalThis, 'fetch').mockRejectedValue(new TypeError('Failed to fetch'));

  const rootRoute = createRootRoute();
  const orgRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/o/$org',
    beforeLoad: () => ({ org: current }),
    component: () => (
      <Menu open>
        <MenuTrigger>Open</MenuTrigger>
        <OrgMenuContent org={current} onCreate={onCreate} navigating={{ current: false }} />
        <Outlet />
      </Menu>
    ),
  });
  const pages = ['members', 'api-keys', 'settings'].map((path) =>
    createRoute({ getParentRoute: () => orgRoute, path, component: () => <p>{path} page</p> }),
  );
  const home = createRoute({ getParentRoute: () => orgRoute, path: '/', component: () => <p>Home page</p> });
  const router = createRouter({
    routeTree: rootRoute.addChildren([orgRoute.addChildren([home, ...pages])]),
    history: createMemoryHistory({ initialEntries: ['/o/goal'] }),
  });

  render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <RouterProvider router={router} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return { client, org };
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

const menu = () => screen.findByRole('menu');

beforeEach(stubMatchMedia);

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe('the organization menu', () => {
  it('lists each organization with its factory count and role, and checks the current one', async () => {
    renderMenu();
    const m = await menu();

    const current = within(m).getByRole('menuitem', { name: /^Goal\s?1 factory, owner/ });
    expect(current.getAttribute('aria-current')).toBe('true');
    expect(within(current).getByLabelText('Current')).toBeTruthy();

    const other = within(m).getByRole('menuitem', { name: /^Acme\s?3 factories, owner/ });
    expect(other.getAttribute('aria-current')).toBeNull();
    expect(within(other).queryByLabelText('Current')).toBeNull();
  });

  it('offers Create organization to whoever may create one, and calls onCreate', async () => {
    const onCreate = vi.fn();
    renderMenu({ onCreate });
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Create organization' }));
    expect(onCreate).toHaveBeenCalledTimes(1);
  });

  it('hides Create organization from someone who may not create one', async () => {
    renderMenu({ platformAdmin: false });
    await menu();
    expect(screen.queryByRole('menuitem', { name: 'Create organization' })).toBeNull();
  });

  it('names the organization in its section, with the member count once loaded', async () => {
    renderMenu();
    const m = await menu();
    expect(within(m).getByRole('group', { name: 'Goal' })).toBeTruthy();
    const members = within(m).getByRole('menuitem', { name: /^Members and roles/ });
    expect(members.textContent).toContain('4');
    expect(within(m).getByRole('menuitem', { name: 'API keys' }).getAttribute('href')).toBe('/o/goal/api-keys');
    expect(within(m).getByRole('menuitem', { name: 'Organization settings' }).getAttribute('href')).toBe(
      '/o/goal/settings',
    );
  });

  it('renders Spend and billing disabled, with the not-available tooltip', async () => {
    renderMenu();
    const m = await menu();
    const item = within(m).getByRole('menuitem', { name: 'Spend and billing' });
    expect(item.getAttribute('aria-disabled')).toBe('true');
    fireEvent.focus(item);
    expect(await screen.findByText('Not available yet')).toBeTruthy();
  });

  it("links Models and policies to the organization's page", async () => {
    renderMenu();
    const m = await menu();
    expect(within(m).getByRole('menuitem', { name: 'Models and policies' }).getAttribute('href')).toBe(
      '/o/goal/models',
    );
  });

  it('shows the Members count only while the members list loads, then the count', async () => {
    renderMenu({ members: 'pending' });
    const m = await menu();
    const members = within(m).getByRole('menuitem', { name: /^Members and roles/ });
    expect(within(members).getByText('Loading')).toBeTruthy();
    expect(members.querySelector('[aria-busy="true"]')).not.toBeNull();
  });

  it('leaves the count out when the members list fails, and keeps the row usable', async () => {
    renderMenu({ members: 'error' });
    const m = await menu();
    const members = await within(m).findByRole('menuitem', { name: /^Members and roles/ });
    await waitFor(() => expect(members.textContent).toBe('Members and roles'));
    expect(members.getAttribute('href')).toBe('/o/goal/members');
  });

  it('is empty of other organizations when the caller is in only the current one', async () => {
    renderMenu({ orgs: [goal] });
    const m = await menu();
    expect(within(m).getAllByRole('menuitem', { name: /factor(y|ies)/ })).toHaveLength(1);
  });

  it('does not request the members list without the member:read grant', async () => {
    const { client, org } = renderMenu({ can: ['api_key:read'], members: 'none' });
    const m = await menu();
    expect(within(m).queryByRole('menuitem', { name: /^Members and roles/ })).toBeNull();
    expect(client.getQueryState(membersQuery(org.id).queryKey)).toBeUndefined();
  });
});
