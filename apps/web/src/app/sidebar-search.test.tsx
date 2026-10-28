// @vitest-environment jsdom

import { ApiError, type Problem } from '@goal/api-client';
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
import { attentionQuery, factoriesQuery, meQuery, orgQuery, orgsQuery, overviewQuery } from '../api/queries';
import { searchOrg } from '../api/search';
import type {
  Factory,
  FactoryOverviewList,
  Me,
  Org,
  OrgAttention,
  OrgSearch,
  OrgSummary,
  SearchItem,
} from '../api/types';
import { TooltipProvider } from '../ui/tooltip';
import { AccountProvider } from './account-menu';
import { Sidebar } from './sidebar';

// The organization's search answers, per query: the tests set what the service returns.
vi.mock('../api/search', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api/search')>()),
  searchOrg: vi.fn(),
}));

const summary: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'Ac', factory_count: 3, role: 'owner' };

const factory = (name: string, title?: string): Factory =>
  ({
    id: `${name}-id`,
    org_id: 'org-1',
    name,
    title,
    work_types: [],
    stations: [],
    current: null,
    draft: null,
    paused: false,
    created_at: '2026-10-01T00:00:00Z',
    role: 'editor',
    can: [],
  }) as Factory;

const factories = [factory('goal', 'Software'), factory('sre'), factory('support')];

const quiet = { waiting_on_you: 0, blocked_items: 0, andon: null, connections_blocked: 0 };
const attention: OrgAttention = {
  waiting_on_you: 0,
  factories: factories.map((f) => ({ factory_id: f.id, ...quiet })),
};

const overview: FactoryOverviewList = {
  window: '7d',
  since: '2026-10-21',
  until: '2026-10-27',
  waiting_on_you: 0,
  spend_usd: 512,
  budget_usd: null,
  factories: [],
};

const me = {
  kind: 'user',
  mode: 'session',
  platform_admin: false,
  user: { id: 'user-1', name: 'Freddy Cruz', email: 'freddy@example.com' },
} as Me;

const item = (number: number, title: string, station: string, factoryName = 'goal'): SearchItem => ({
  id: `item-${factoryName}-${number}`,
  number,
  factory_id: `${factoryName}-id`,
  factory_name: factoryName,
  title,
  state: 'in_progress',
  station,
});

/** What the service answers for "spend" in acme: the three spend items, an agent, a connection and a factory. */
const spend: OrgSearch = {
  items: [
    item(147, "Show this week's spend", 'build'),
    item(145, 'Check the spend against the budget', 'build'),
    item(9, 'S3 spend up 22% month over month', 'investigate', 'sre'),
  ],
  agents: [{ name: 'builder', stations: ['build'], factory_id: 'goal-id', factory_name: 'goal' }],
  factories: [],
  connections: [{ name: 'acme-spend-warehouse', kind: 'github' }],
};

const empty: OrgSearch = { items: [], agents: [], factories: [], connections: [] };

interface Setup {
  path?: string;
}

/** The sidebar under the organization's route; the item, the Floor and the other pages are plain placeholders. */
function renderSidebar({ path = '/o/acme' }: Setup = {}) {
  const org: Org = { ...summary, created_at: '2026-09-01T00:00:00Z', can: [] } as Org;
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(orgQuery(org.id).queryKey, org);
  client.setQueryData(factoriesQuery(org.id).queryKey, factories);
  client.setQueryData(meQuery.queryKey, me);
  client.setQueryData(orgsQuery.queryKey, [org]);
  client.setQueryData(attentionQuery(org.id).queryKey, attention);
  client.setQueryData(overviewQuery(org.id).queryKey, overview);

  const rootRoute = createRootRoute();
  const orgRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/o/$org',
    beforeLoad: () => ({ org: summary }),
    component: () => (
      <AccountProvider>
        <Sidebar org={summary} />
        <Outlet />
      </AccountProvider>
    ),
  });
  const home = createRoute({ getParentRoute: () => orgRoute, path: '/', component: () => <p>Home page</p> });
  const members = createRoute({ getParentRoute: () => orgRoute, path: '/members', component: () => <p>Members</p> });
  const factoryRoute = createRoute({ getParentRoute: () => orgRoute, path: '/f/$factory', component: Outlet });
  const floor = createRoute({ getParentRoute: () => factoryRoute, path: '/', component: () => <p>Floor page</p> });
  const item = createRoute({
    getParentRoute: () => factoryRoute,
    path: 'items/$ref',
    component: () => <p>Item page</p>,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([orgRoute.addChildren([home, members, factoryRoute.addChildren([floor, item])])]),
    history: createMemoryHistory({ initialEntries: [path] }),
  });

  render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <RouterProvider router={router} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return { router };
}

const nav = () => screen.findByRole('navigation', { name: 'Organization and factories' });

/** Opens search with the keyboard, as the shortcut does on macOS. */
function pressCommandK(platform: 'MacIntel' | '' = 'MacIntel') {
  Object.defineProperty(navigator, 'platform', { value: platform, configurable: true });
  fireEvent.keyDown(window, { key: 'k', metaKey: platform === 'MacIntel', ctrlKey: platform === '' });
}

/** Types into the search input and waits for the debounced search to be sent. */
async function type(value: string) {
  fireEvent.change(screen.getByRole('combobox', { name: 'Search' }), { target: { value } });
  await waitFor(() =>
    expect(vi.mocked(searchOrg)).toHaveBeenLastCalledWith('org-1', { q: value.trim(), limit: 5 }, expect.anything()),
  );
  await waitFor(() => expect(screen.queryByText('Loading')).toBeNull());
}

const platformDescriptor = Object.getOwnPropertyDescriptor(navigator, 'platform');

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
  if (platformDescriptor) Object.defineProperty(navigator, 'platform', platformDescriptor);
});

describe('search from the sidebar', () => {
  it('opens from the Search icon and swaps the top row for a labelled input', async () => {
    renderSidebar();
    const sidebar = await nav();

    fireEvent.click(within(sidebar).getByRole('button', { name: 'Search' }));

    const input = within(sidebar).getByRole('combobox', { name: 'Search' });
    expect(document.activeElement).toBe(input);
    expect(within(sidebar).queryByRole('button', { name: 'Collapse sidebar' })).toBeNull();
    expect(within(sidebar).getByRole('button', { name: 'Close search' })).toBeTruthy();
  });

  it('opens on ⌘K on macOS and on Ctrl+K elsewhere', async () => {
    renderSidebar();
    await nav();

    pressCommandK('MacIntel');
    expect(screen.getByRole('combobox', { name: 'Search' })).toBeTruthy();
    cleanup();
    renderSidebar();
    await nav();

    pressCommandK('');
    expect(screen.getByRole('combobox', { name: 'Search' })).toBeTruthy();
  });

  it('closes on Escape and returns focus to the Search icon', async () => {
    renderSidebar();
    await nav();
    pressCommandK();

    fireEvent.keyDown(screen.getByRole('combobox', { name: 'Search' }), { key: 'Escape' });

    expect(screen.queryByRole('combobox', { name: 'Search' })).toBeNull();
    expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Search' }));
  });

  it('closes from Close search and returns focus to the Search icon', async () => {
    renderSidebar();
    await nav();
    pressCommandK();

    fireEvent.click(screen.getByRole('button', { name: 'Close search' }));

    expect(screen.queryByRole('combobox', { name: 'Search' })).toBeNull();
    expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Search' }));
  });

  it('sends one search for a pause in typing, and none for a blank query', async () => {
    vi.mocked(searchOrg).mockResolvedValue(spend);
    renderSidebar();
    await nav();
    pressCommandK();

    const input = screen.getByRole('combobox', { name: 'Search' });
    fireEvent.change(input, { target: { value: 's' } });
    fireEvent.change(input, { target: { value: 'spend' } });
    await waitFor(() => expect(searchOrg).toHaveBeenCalledTimes(1));
    expect(searchOrg).toHaveBeenCalledWith('org-1', { q: 'spend', limit: 5 }, expect.anything());

    fireEvent.change(input, { target: { value: '   ' } });
    await new Promise((resolve) => setTimeout(resolve, 250));
    expect(searchOrg).toHaveBeenCalledTimes(1);
  });

  it('moves the highlighted row across the groups with the arrow keys and opens it with Enter', async () => {
    vi.mocked(searchOrg).mockResolvedValue(spend);
    const { router } = renderSidebar();
    await nav();
    pressCommandK();
    const input = screen.getByRole('combobox', { name: 'Search' });
    await type('spend');

    const [first, second, third, fourth] = screen.getAllByRole('option');
    expect(first?.getAttribute('aria-selected')).toBe('true');
    expect(input.getAttribute('aria-activedescendant')).toBe(first?.id);

    fireEvent.keyDown(input, { key: 'ArrowDown' });
    expect(second?.getAttribute('aria-selected')).toBe('true');
    expect(input.getAttribute('aria-activedescendant')).toBe(second?.id);

    fireEvent.keyDown(input, { key: 'ArrowUp' });
    expect(first?.getAttribute('aria-selected')).toBe('true');

    fireEvent.keyDown(input, { key: 'ArrowDown' });
    fireEvent.keyDown(input, { key: 'ArrowDown' });
    expect(third?.getAttribute('aria-selected')).toBe('true');
    // The agent and the connection come after the items, so the highlight moves into the next group.
    fireEvent.keyDown(input, { key: 'ArrowDown' });
    expect(fourth?.getAttribute('aria-selected')).toBe('true');
    expect(within(screen.getByRole('group', { name: 'Agents' })).getByRole('option')).toBe(fourth);

    fireEvent.keyDown(input, { key: 'ArrowUp' });
    fireEvent.keyDown(input, { key: 'ArrowUp' });
    fireEvent.keyDown(input, { key: 'ArrowUp' });
    fireEvent.keyDown(input, { key: 'ArrowUp' });
    expect(first?.getAttribute('aria-selected')).toBe('true');
    fireEvent.keyDown(input, { key: 'Enter' });
    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/f/goal/items/147'));
  });

  it('groups results as Items, Pages, Agents, Connections and Factories, and leaves out an empty group', async () => {
    vi.mocked(searchOrg).mockResolvedValue({ ...spend, factories: [{ id: 'sre-id', name: 'sre', title: 'SRE' }] });
    renderSidebar();
    await nav();
    pressCommandK();
    await type('spend');

    const headings = screen.getAllByRole('group').map((g) => g.querySelector('legend')?.textContent);
    expect(headings).toEqual(['Items', 'Agents', 'Connections', 'Factories']);
    expect(screen.queryByRole('group', { name: 'Pages' })).toBeNull();

    const items = within(screen.getByRole('group', { name: 'Items' })).getAllByRole('option');
    expect(items.map((o) => o.textContent)).toEqual([
      expect.stringContaining("#147 Show this week's spend"),
      expect.stringContaining('#145 Check the spend against the budget'),
      expect.stringContaining('#9 S3 spend up 22% month over month'),
    ]);
    // Each item shows its factory letter and the station it is at, on the right.
    expect(items[0]?.textContent).toMatch(/^G#147/);
    expect(items[0]?.textContent).toContain('Build');
    expect(screen.getByRole('option', { name: 'builder, goal' }).textContent).toContain('goal');
    expect(screen.getByRole('option', { name: /acme-spend-warehouse/ })).toBeTruthy();
    expect(screen.getByRole('option', { name: 'SRE' })).toBeTruthy();
  });

  it('matches the pages of the registry on the client, with the context each one shows', async () => {
    vi.mocked(searchOrg).mockResolvedValue(empty);
    const { router } = renderSidebar();
    await nav();
    pressCommandK();
    await type('members');

    const pages = screen.getByRole('group', { name: 'Pages' });
    const page = within(pages).getByRole('option', { name: 'Members and roles' });
    expect(page.textContent).toContain('Organization');

    fireEvent.keyDown(screen.getByRole('combobox', { name: 'Search' }), { key: 'Enter' });
    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/members'));
  });

  it("shows the open factory's Settings as Settings, and its other pages by the factory's title", async () => {
    vi.mocked(searchOrg).mockResolvedValue(empty);
    renderSidebar({ path: '/o/acme/f/goal' });
    await nav();
    pressCommandK();
    await type('settings');

    const pages = within(screen.getByRole('group', { name: 'Pages' }));
    expect(pages.getAllByRole('option', { name: 'Settings' })).toHaveLength(2);
    expect(pages.getAllByRole('option').map((o) => o.textContent)).toEqual([
      expect.stringContaining('Organization'),
      expect.stringContaining('Settings'),
    ]);

    await type('floor');
    expect(screen.getByRole('option', { name: 'Floor' }).textContent).toContain('Software');
  });

  it('opens an agent on the Floor of its factory, since the agent page is not built yet', async () => {
    vi.mocked(searchOrg).mockResolvedValue({ ...empty, agents: spend.agents });
    const { router } = renderSidebar();
    await nav();
    pressCommandK();
    await type('builder');

    fireEvent.mouseDown(screen.getByRole('option', { name: 'builder, goal' }));
    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/f/goal'));
  });

  it('opens a factory on its Floor', async () => {
    vi.mocked(searchOrg).mockResolvedValue({ ...empty, factories: [{ id: 'sre-id', name: 'sre', title: 'SRE' }] });
    const { router } = renderSidebar();
    await nav();
    pressCommandK();
    await type('sre');

    fireEvent.mouseDown(screen.getByRole('option', { name: 'SRE' }));
    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/f/sre'));
  });

  it('says nothing matches, in the query’s own words, and lists no group', async () => {
    vi.mocked(searchOrg).mockResolvedValue(empty);
    renderSidebar();
    await nav();
    pressCommandK();
    await type('zzz');

    expect(screen.getByText('Nothing matches "zzz".')).toBeTruthy();
    expect(screen.queryAllByRole('option')).toHaveLength(0);
    expect(screen.queryByRole('listbox')).toBeNull();
  });

  it('is busy and says Loading while the search is sent', async () => {
    vi.mocked(searchOrg).mockReturnValue(new Promise(() => {}));
    renderSidebar();
    await nav();
    pressCommandK();
    fireEvent.change(screen.getByRole('combobox', { name: 'Search' }), { target: { value: 'spend' } });

    await waitFor(() => expect(screen.getByText('Loading')).toBeTruthy());
    const region = screen.getByText('Loading').closest('[aria-busy]');
    expect(region?.getAttribute('aria-busy')).toBe('true');
    expect(screen.queryAllByRole('option')).toHaveLength(0);
    expect(screen.queryByText(/Nothing matches/)).toBeNull();
  });

  it('says the search failed, and Retry sends it again', async () => {
    // A 4xx answer is not retried by the query, so the error shows at once.
    vi.mocked(searchOrg).mockRejectedValueOnce(new ApiError({ title: 'Bad Request', status: 400 } as Problem));
    renderSidebar();
    await nav();
    pressCommandK();
    fireEvent.change(screen.getByRole('combobox', { name: 'Search' }), { target: { value: 'spend' } });
    expect(await screen.findByText('Could not search.')).toBeTruthy();

    // The service answers now: Retry sends the search again, and its matches appear.
    vi.mocked(searchOrg).mockResolvedValue(spend);
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));

    expect(await screen.findAllByRole('option')).toHaveLength(5);
    expect(screen.queryByText('Could not search.')).toBeNull();
  });

  it('shows the footer hint for the keys', async () => {
    renderSidebar();
    await nav();
    pressCommandK();

    expect(
      screen.getByText((_, el) => el?.tagName === 'P' && el.textContent === '↑ ↓ to move, ↵ to open'),
    ).toBeTruthy();
  });
});
