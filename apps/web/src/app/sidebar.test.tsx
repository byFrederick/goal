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
import { afterEach, describe, expect, it, vi } from 'vitest';
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
import { Sidebar } from './sidebar';

const summary: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'Ac', factory_count: 3, role: 'owner' };

const factory = (name: string, extra: Partial<Factory> = {}): Factory => ({
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
  can: [],
  ...extra,
});

const factories = [factory('goal'), factory('sre'), factory('support')];

const quiet = { waiting_on_you: 0, blocked_items: 0, andon: null, connections_blocked: 0 };

const attention: OrgAttention = {
  waiting_on_you: 3,
  factories: [
    { factory_id: 'goal-id', ...quiet },
    { factory_id: 'sre-id', ...quiet, waiting_on_you: 3 },
    {
      factory_id: 'support-id',
      ...quiet,
      andon: {
        id: 1,
        kind: 'manual',
        station: null,
        reason: 'Answers sent back 4 of the last 10 times',
        pulled_by: 'user:local',
        pulled_at: '2026-10-27T09:00:00Z',
        cleared_by: null,
        cleared_at: null,
        note: null,
        data: {},
      },
    },
  ],
};

const spend = (budget: number | null): FactoryOverviewList => ({
  window: '7d',
  since: '2026-10-21',
  until: '2026-10-27',
  waiting_on_you: 3,
  spend_usd: 512,
  budget_usd: budget,
  factories: [],
});

const me: Me = {
  kind: 'user',
  mode: 'session',
  platform_admin: false,
  user: { id: 'user-1', name: 'Freddy Cruz', email: 'freddy@example.com' },
} as Me;

interface Setup {
  at?: string;
  can?: string[];
  list?: Factory[];
  attention?: OrgAttention | 'pending' | 'error';
  overview?: FactoryOverviewList | 'pending' | 'error';
}

/** The sidebar under the organization's route, at `at`; the factory's pages are plain placeholders. */
function renderSidebar({
  at = '/o/acme',
  can = [],
  list = factories,
  attention: att = attention,
  overview: ov = spend(1400),
}: Setup = {}) {
  const org: Org = { ...summary, created_at: '2026-09-01T00:00:00Z', can } as Org;
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(orgQuery(org.id).queryKey, org);
  client.setQueryData(factoriesQuery(org.id).queryKey, list);
  client.setQueryData(meQuery.queryKey, me);
  client.setQueryData(orgsQuery.queryKey, [org]);
  client.setQueryData(connectionsQuery(org.id).queryKey, []);
  if (att === 'pending') vi.spyOn(globalThis, 'fetch').mockImplementation(() => new Promise(() => {}));
  else if (att === 'error') vi.spyOn(globalThis, 'fetch').mockRejectedValue(new TypeError('Failed to fetch'));
  else client.setQueryData(attentionQuery(org.id).queryKey, att);
  if (ov === 'pending') vi.spyOn(globalThis, 'fetch').mockImplementation(() => new Promise(() => {}));
  else if (ov === 'error') vi.spyOn(globalThis, 'fetch').mockRejectedValue(new TypeError('Failed to fetch'));
  else client.setQueryData(overviewQuery(org.id).queryKey, ov);

  const rootRoute = createRootRoute();
  const orgRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/o/$org',
    beforeLoad: () => ({ org: summary }),
    component: () => (
      <AccountProvider org={summary}>
        <Sidebar org={summary} />
        <Outlet />
      </AccountProvider>
    ),
  });
  const home = createRoute({ getParentRoute: () => orgRoute, path: '/', component: () => <p>Home page</p> });
  const factoryRoute = createRoute({ getParentRoute: () => orgRoute, path: '/f/$factory', component: Outlet });
  const floor = createRoute({ getParentRoute: () => factoryRoute, path: '/', component: () => <p>Floor page</p> });
  const board = createRoute({ getParentRoute: () => factoryRoute, path: 'board', component: () => <p>Board page</p> });
  const router = createRouter({
    routeTree: rootRoute.addChildren([orgRoute.addChildren([home, factoryRoute.addChildren([floor, board])])]),
    history: createMemoryHistory({ initialEntries: [at] }),
  });

  const rendered = render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <RouterProvider router={router} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return { ...rendered, router };
}

const nav = () => screen.findByRole('navigation', { name: 'Organization and factories' });

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  window.localStorage.clear();
});

const collapse = async () => {
  fireEvent.click(within(await nav()).getByRole('button', { name: 'Collapse sidebar' }));
  return screen.findByRole('button', { name: 'Expand sidebar' });
};

/** The names of the rail's controls and images, in the order they sit on the rail. */
const railNames = (rail: HTMLElement) =>
  Array.from(rail.querySelectorAll('a[aria-label], button[aria-label], [role="img"]')).map((el) =>
    el.getAttribute('aria-label'),
  );

describe('the sidebar', () => {
  it('names the organization in its top row, which opens the organization menu', async () => {
    renderSidebar();

    const top = await screen.findByRole('button', { name: 'Acme organization, switch organization' });
    expect(top.textContent).toContain('Acme');
    expect(within(await nav()).getByRole('button', { name: 'Search' })).toBeTruthy();
    expect(within(await nav()).getByRole('button', { name: 'Collapse sidebar' })).toBeTruthy();
  });

  it('lists the organization-wide pages in the sidebar’s order', async () => {
    renderSidebar({ can: ['connection:read', 'knowledge:read'] });
    const links = within(await nav())
      .getAllByRole('link', { hidden: false })
      .map((link) => link.textContent);

    expect(links.findIndex((text) => text?.startsWith('Home'))).toBeLessThan(
      links.findIndex((text) => text?.startsWith('Connections')),
    );
    expect(links.some((text) => text?.startsWith('Knowledge'))).toBe(true);
  });

  it.each([
    ['Search', /^Search$/],
    ['New item', /^New item/],
    ['Needs you', /^Needs you/],
    ['Runs', /^Runs$/],
    ['Credentials', /^Credentials$/],
  ])('renders %s as not available: aria-disabled, focusable, and inert', async (_, name) => {
    const { router } = renderSidebar({ can: ['connection:read', 'knowledge:read'] });
    const control = within(await nav()).getByRole('button', { name });

    expect(control.getAttribute('aria-disabled')).toBe('true');
    expect(control.tabIndex).not.toBe(-1);
    fireEvent.click(control);
    expect(router.state.location.pathname).toBe('/o/acme');
  });

  it('shows the not-available tooltip on hover', async () => {
    renderSidebar();
    fireEvent.focus(within(await nav()).getByRole('button', { name: 'Runs' }));

    expect(await screen.findByText('Not available yet')).toBeTruthy();
  });

  it('keeps Agents, Intake and Jonah disabled in the open factory', async () => {
    renderSidebar({ at: '/o/acme/f/goal' });
    const sidebar = await nav();

    for (const name of ['Agents', 'Intake', 'Jonah']) {
      expect(within(sidebar).getByRole('button', { name }).getAttribute('aria-disabled')).toBe('true');
    }
  });

  it('shows New factory only to whoever may create one', async () => {
    const { unmount } = renderSidebar({ can: ['connection:read'] });
    expect(within(await nav()).queryByRole('link', { name: 'New factory' })).toBeNull();
    unmount();

    renderSidebar({ can: ['factory:create'] });
    expect(within(await nav()).getByRole('link', { name: 'New factory' })).toBeTruthy();
  });

  it('expands only the open factory, with its current page as the white chip', async () => {
    renderSidebar({ at: '/o/acme/f/goal' });
    const sidebar = await nav();

    expect(within(sidebar).getAllByRole('link', { name: 'Floor' })).toHaveLength(1);
    expect(within(sidebar).getByRole('link', { name: 'Floor' }).getAttribute('aria-current')).toBe('page');
    expect(within(sidebar).getByRole('link', { name: 'Board' }).getAttribute('aria-current')).toBeNull();
    expect(within(sidebar).queryAllByRole('link', { name: 'Floor' })).toHaveLength(1);
  });

  it('marks Home on the organization’s own page', async () => {
    renderSidebar({ at: '/o/acme' });

    expect((await nav()).querySelector('a[aria-current="page"]')?.textContent).toBe('Home');
  });

  it('says what waits on a person and marks the factory with an andon pull', async () => {
    renderSidebar();
    const sidebar = await nav();

    expect(within(sidebar).getByRole('button', { name: /Needs you/ }).textContent).toContain('waiting on you');
    expect(within(sidebar).getByRole('link', { name: /sre factory, 3 waiting on you/ })).toBeTruthy();
    expect(within(sidebar).getByRole('link', { name: /support factory.*andon pulled/ })).toBeTruthy();
  });

  it('shows the spend this week against the budget, and is not a link', async () => {
    renderSidebar();
    const sidebar = await nav();

    expect(within(sidebar).getByText('Spend this week')).toBeTruthy();
    expect(within(sidebar).getByText('$512 of $1,400')).toBeTruthy();
    expect(within(sidebar).queryByRole('link', { name: /Spend/ })).toBeNull();
  });

  it('shows the amount alone when no factory has a budget', async () => {
    renderSidebar({ overview: spend(null) });
    const sidebar = await nav();

    expect(within(sidebar).getByText('$512')).toBeTruthy();
    expect(within(sidebar).queryByText(/of/)).toBeNull();
  });

  it('shows the person and their role, and opens the account menu', async () => {
    renderSidebar();
    const sidebar = await nav();

    expect(within(sidebar).getByRole('button', { name: 'FC, Freddy Cruz, account' }).textContent).toContain('Owner');
  });

  it('renders loading as busy with a spoken “Loading” and no placeholder', async () => {
    renderSidebar({ attention: 'pending', overview: 'pending' });
    const sidebar = await nav();

    expect(sidebar.getAttribute('aria-busy')).toBe('true');
    expect(within(sidebar).getByText('Loading')).toBeTruthy();
    expect(within(sidebar).queryByText('Spend this week')).toBeNull();
  });

  it('leaves the counts and the spend out on an error, and keeps the rest of the nav', async () => {
    renderSidebar({ attention: 'error', overview: 'error' });
    const sidebar = await nav();

    await waitFor(() => expect(sidebar.getAttribute('aria-busy')).toBe('false'));
    expect(within(sidebar).queryByText('Spend this week')).toBeNull();
    expect(within(sidebar).queryByText(/waiting on you/)).toBeNull();
    expect(within(sidebar).getByRole('button', { name: 'Needs you' })).toBeTruthy();
    expect(within(sidebar).getByRole('link', { name: /sre factory/ })).toBeTruthy();
  });

  it('shows only the Factories label and its + with no factories', async () => {
    renderSidebar({ list: [], can: ['factory:create'] });
    const sidebar = await nav();

    expect(within(sidebar).getByText('Factories')).toBeTruthy();
    expect(within(sidebar).getByRole('link', { name: 'New factory' })).toBeTruthy();
    expect(sidebar.querySelectorAll('button[aria-controls]')).toHaveLength(0);
  });
});

describe('the collapsed rail', () => {
  it('lists its entries in order with accessible names', async () => {
    renderSidebar({ can: ['connection:read', 'knowledge:read', 'factory:create'] });
    await collapse();
    const names = railNames(await nav());

    expect(names.slice(0, 8)).toEqual([
      'Expand sidebar',
      'New item',
      'Home',
      'Needs you, 3 waiting on you',
      'Runs',
      'Connections',
      'Credentials',
      'Knowledge',
    ]);
    expect(names.slice(8, 11).map((name) => name?.match(/, (\w+) factory/)?.[1])).toEqual(['goal', 'sre', 'support']);
    expect(names.slice(11)).toEqual(['New factory', 'Spend this week, 37% of budget', 'FC, Freddy Cruz, account']);
  });

  it.each(['New item', 'Needs you', 'Runs', 'Credentials'])('keeps %s disabled, and says why', async (name) => {
    renderSidebar();
    await collapse();
    const control = within(await nav()).getByRole('button', { name: new RegExp(`^${name}`) });

    expect(control.getAttribute('aria-disabled')).toBe('true');
    fireEvent.focus(control);
    expect(await screen.findByText('Not available yet')).toBeTruthy();
  });

  it('gives each enabled entry a tooltip with its label', async () => {
    renderSidebar({ can: ['connection:read'] });
    await collapse();
    fireEvent.focus(within(await nav()).getByRole('link', { name: 'Connections' }));

    expect(await screen.findByText('Connections')).toBeTruthy();
  });

  it('the collapsed choice survives a remount', async () => {
    const { unmount } = renderSidebar();
    await collapse();
    unmount();

    renderSidebar();
    const expand = await screen.findByRole('button', { name: 'Expand sidebar' });
    expect(within(await nav()).queryByRole('button', { name: 'Collapse sidebar' })).toBeNull();
    fireEvent.click(expand);
    expect(await screen.findByRole('button', { name: 'Collapse sidebar' })).toBeTruthy();
    unmount();

    renderSidebar();
    expect(await screen.findByRole('button', { name: 'Collapse sidebar' })).toBeTruthy();
  });

  it('moves focus to the other control when the choice changes by the keyboard', async () => {
    renderSidebar();
    const expand = await collapse();
    expect(document.activeElement).toBe(expand);

    fireEvent.click(expand);
    expect(document.activeElement).toBe(await screen.findByRole('button', { name: 'Collapse sidebar' }));
  });

  it('expands when the saved choice cannot be read', async () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    renderSidebar();

    expect(await screen.findByRole('button', { name: 'Collapse sidebar' })).toBeTruthy();
  });

  it('renders loading as busy with a spoken “Loading” and no spend ring', async () => {
    window.localStorage.setItem('goal.sidebar', 'collapsed');
    renderSidebar({ attention: 'pending', overview: 'pending' });
    const rail = await nav();

    expect(rail.getAttribute('aria-busy')).toBe('true');
    expect(within(rail).getByText('Loading')).toBeTruthy();
    expect(within(rail).queryByRole('img', { name: /Spend/ })).toBeNull();
  });

  it('shows the icons without counts and no spend ring on an error', async () => {
    window.localStorage.setItem('goal.sidebar', 'collapsed');
    renderSidebar({ attention: 'error', overview: 'error' });
    const rail = await nav();

    await waitFor(() => expect(rail.getAttribute('aria-busy')).toBe('false'));
    expect(within(rail).getByRole('button', { name: 'Needs you' })).toBeTruthy();
    expect(within(rail).queryByRole('img', { name: /Spend/ })).toBeNull();
    expect(within(rail).getByRole('link', { name: /sre factory/ })).toBeTruthy();
  });

  it('shows only New factory with no factories', async () => {
    window.localStorage.setItem('goal.sidebar', 'collapsed');
    renderSidebar({ list: [], can: ['factory:create'] });
    const rail = await nav();

    expect(
      within(rail)
        .getAllByRole('link', { name: /factory/ })
        .map((el) => el.getAttribute('aria-label')),
    ).toEqual(['New factory']);
  });
});
