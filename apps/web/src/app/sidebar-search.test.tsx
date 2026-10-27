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
import { listItems } from '../api/client';
import {
  attentionQuery,
  factoriesQuery,
  itemsQuery,
  meQuery,
  orgQuery,
  orgsQuery,
  overviewQuery,
} from '../api/queries';
import type { Factory, FactoryOverviewList, ItemCard, Me, Org, OrgAttention, OrgSummary } from '../api/types';
import { TooltipProvider } from '../ui/tooltip';
import { AccountProvider } from './account-menu';
import { Sidebar } from './sidebar';

// The items the search fetches, per factory: pending, failing, or answering with the seeded cards.
vi.mock('../api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api/client')>()),
  listItems: vi.fn(),
}));

const summary: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'Ac', factory_count: 3, role: 'owner' };

const factory = (name: string): Factory => ({
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
});

const factories = [factory('goal'), factory('sre'), factory('support')];

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

/** An item card as listItems returns it; only what the search shows matters here. */
const card = (number: number, title: string, station: string, ref: string): ItemCard =>
  ({ id: `item-${number}`, ref, number, title, station, state: 'in_progress' }) as ItemCard;

const itemsByFactory: Record<string, ItemCard[]> = {
  'goal-id': [
    card(147, "Show this week's spend", 'build', 'goal#147'),
    card(145, 'Check the spend against the budget', 'build', 'goal#145'),
    card(146, 'Upgrade the tests', 'review', 'goal#146'),
  ],
  'sre-id': [card(9, 'S3 spend up 22% month over month', 'investigate', 'sre#9')],
  'support-id': [],
};

interface Setup {
  items?: 'ready' | 'pending' | 'error';
}

/** The sidebar under the organization's route; the item page is a plain placeholder. */
function renderSidebar({ items = 'ready' }: Setup = {}) {
  const org: Org = { ...summary, created_at: '2026-09-01T00:00:00Z', can: [] } as Org;
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(orgQuery(org.id).queryKey, org);
  client.setQueryData(factoriesQuery(org.id).queryKey, factories);
  client.setQueryData(meQuery.queryKey, me);
  client.setQueryData(orgsQuery.queryKey, [org]);
  client.setQueryData(attentionQuery(org.id).queryKey, attention);
  client.setQueryData(overviewQuery(org.id).queryKey, overview);
  if (items === 'ready') {
    for (const f of factories) {
      client.setQueryData(itemsQuery(org.id, f.id, { limit: 200 }).queryKey, {
        items: itemsByFactory[f.id] ?? [],
        next_after: null,
      });
    }
  } else if (items === 'pending') {
    vi.mocked(listItems).mockImplementation(() => new Promise(() => {}));
  } else {
    vi.mocked(listItems).mockRejectedValue(new TypeError('Failed to fetch'));
  }

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
  const factoryRoute = createRoute({ getParentRoute: () => orgRoute, path: '/f/$factory', component: Outlet });
  const floor = createRoute({ getParentRoute: () => factoryRoute, path: '/', component: () => <p>Floor page</p> });
  const item = createRoute({
    getParentRoute: () => factoryRoute,
    path: 'items/$ref',
    component: () => <p>Item page</p>,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([orgRoute.addChildren([home, factoryRoute.addChildren([floor, item])])]),
    history: createMemoryHistory({ initialEntries: ['/o/acme'] }),
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

const platformDescriptor = Object.getOwnPropertyDescriptor(navigator, 'platform');

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
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

  it('moves the highlighted row with the arrow keys and opens it with Enter', async () => {
    const { router } = renderSidebar();
    await nav();
    pressCommandK();
    const input = screen.getByRole('combobox', { name: 'Search' });

    fireEvent.change(input, { target: { value: 'spend' } });
    const [first, second, third] = screen.getAllByRole('option');
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
    fireEvent.keyDown(input, { key: 'ArrowDown' });
    expect(third?.getAttribute('aria-selected')).toBe('true');

    fireEvent.keyDown(input, { key: 'ArrowUp' });
    fireEvent.keyDown(input, { key: 'ArrowUp' });
    fireEvent.keyDown(input, { key: 'ArrowUp' });
    expect(first?.getAttribute('aria-selected')).toBe('true');
    fireEvent.keyDown(input, { key: 'Enter' });
    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/f/goal/items/147'));
  });

  it('groups results as Items, Pages and Actions, and matches items by title or #number', async () => {
    renderSidebar();
    await nav();
    pressCommandK();

    fireEvent.change(screen.getByRole('combobox', { name: 'Search' }), { target: { value: 'spend' } });
    const options = screen.getAllByRole('option');
    expect(options.map((o) => o.textContent)).toEqual([
      expect.stringContaining("#147 Show this week's spend"),
      expect.stringContaining('#145 Check the spend against the budget'),
      expect.stringContaining('#9 S3 spend up 22% month over month'),
    ]);
    expect(options[0]?.textContent).toContain('Build');
    for (const group of ['Items', 'Pages', 'Actions']) {
      expect(screen.getByRole('region', { name: group })).toBeTruthy();
    }

    fireEvent.change(screen.getByRole('combobox', { name: 'Search' }), { target: { value: '#146' } });
    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual([
      expect.stringContaining('#146 Upgrade the tests'),
    ]);
  });

  it('shows the Pages and Actions rows as not available that never navigate', async () => {
    const { router } = renderSidebar();
    await nav();
    pressCommandK();
    fireEvent.change(screen.getByRole('combobox', { name: 'Search' }), { target: { value: 'spend' } });

    const page = screen.getByRole('button', { name: /Spend and usage/ });
    expect(page.getAttribute('aria-disabled')).toBe('true');
    expect(page.tabIndex).not.toBe(-1);
    fireEvent.click(page);
    expect(router.state.location.pathname).toBe('/o/acme');
    expect(screen.getByRole('button', { name: /New item about "spend"/ }).getAttribute('aria-disabled')).toBe('true');
  });

  it('says no items match, in the query’s own words', async () => {
    renderSidebar();
    await nav();
    pressCommandK();

    fireEvent.change(screen.getByRole('combobox', { name: 'Search' }), { target: { value: 'zzz' } });

    expect(screen.getByText('No items match "zzz".')).toBeTruthy();
    expect(screen.queryAllByRole('option')).toHaveLength(0);
  });

  it('is busy and says Loading while the items load', async () => {
    renderSidebar({ items: 'pending' });
    await nav();
    pressCommandK();
    fireEvent.change(screen.getByRole('combobox', { name: 'Search' }), { target: { value: 'spend' } });

    const region = screen.getByText('Loading').closest('[aria-busy]');
    expect(region?.getAttribute('aria-busy')).toBe('true');
    expect(screen.queryByText(/No items match/)).toBeNull();
  });

  it('says the items did not load, and Retry fetches them again', async () => {
    renderSidebar({ items: 'error' });
    await nav();
    pressCommandK();
    fireEvent.change(screen.getByRole('combobox', { name: 'Search' }), { target: { value: 'spend' } });
    expect(await screen.findByText('Could not load items.')).toBeTruthy();

    // The service answers now: Retry loads the items, and the matches appear.
    vi.mocked(listItems).mockImplementation(async (_org, factory) => ({
      items: itemsByFactory[`${factory}`] ?? [],
      next_after: null,
    }));
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));

    expect(await screen.findAllByRole('option')).toHaveLength(3);
    expect(screen.queryByText('Could not load items.')).toBeNull();
  });
});
