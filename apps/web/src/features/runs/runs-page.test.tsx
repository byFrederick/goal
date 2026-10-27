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
import { type ReactNode, useState } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { acmeOrg, seed } from '../../../mock/fixtures.ts';
import { orgRunsOf } from '../../../mock/runs.ts';
import type { RunsFilter } from '../../api/client';
import * as client from '../../api/client';
import { factoriesQuery } from '../../api/queries';
import type { OrgRunList, OrgSummary } from '../../api/types';
import { TopBarSetContext, type TopBarState } from '../../app/top-bar';
import { TooltipProvider } from '../../ui/tooltip';
import { RunsPage } from './runs-page';
import { runsSearch } from './search';

// The page reads listOrgRuns; the tests answer it from the mock's own runs, filter by filter, as goald
// would, and read each call to check what the page asked for.
const calls = vi.hoisted(() => ({
  answer: null as null | ((filter: RunsFilter) => OrgRunList | Promise<OrgRunList>),
}));
vi.mock(import('../../api/client'), async (original) => ({
  ...(await original()),
  listOrgRuns: vi.fn(async (_org: string, filter: RunsFilter) => {
    if (!calls.answer) throw new Error('no answer set for listOrgRuns');
    return calls.answer(filter);
  }),
}));

// A fixed day, so the mock's runs fall in "today" (UTC) whatever hour the tests run.
const now = Date.parse('2026-10-27T15:00:00Z');
const today = '2026-10-27T00:00:00.000Z';
const yesterday = '2026-10-26T00:00:00.000Z';

const org: OrgSummary = { id: acmeOrg, slug: 'acme', name: 'Acme', mark: 'A', factory_count: 3, role: null };

const state = seed(now);
const factories = state.factories.filter((f) => f.org_id === acmeOrg);

/** The mock's answer to a filter: its runs in the window the page asked for. */
function fromMock(filter: RunsFilter): OrgRunList {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(filter)) {
    if (value !== undefined) params.set(key, String(value));
  }
  const result = orgRunsOf(state, acmeOrg, params);
  if (!('body' in result)) throw new Error(result.detail);
  return result.body;
}

const emptyList: OrgRunList = {
  runs: [],
  next_cursor: null,
  summary: { total: 0, running: 0, failed: 0, finished: 0, spend_usd: 0, spend_by_factory: [] },
};

/** Renders the page at /o/acme/runs with `search` as its URL, under the real search validation. */
function renderRuns(search = '') {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  queryClient.setQueryData(factoriesQuery(acmeOrg).queryKey, factories);

  const rootRoute = createRootRoute();
  const orgRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/o/$org',
    beforeLoad: () => ({ org }),
    component: Outlet,
  });
  // The app's organization layout is pathless (`_org`), so the runs route's id is the one the page reads.
  const layout = createRoute({ getParentRoute: () => orgRoute, id: '_org', component: Outlet });
  const runsRoute = createRoute({
    getParentRoute: () => layout,
    path: 'runs',
    validateSearch: runsSearch,
    component: RunsPage,
  });
  const routeTree = rootRoute.addChildren([orgRoute.addChildren([layout.addChildren([runsRoute])])]);
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [`/o/acme/runs${search}`] }),
  });

  return {
    ...render(
      <QueryClientProvider client={queryClient}>
        <TooltipProvider>
          <TopBarHarness>
            <RouterProvider router={router} />
          </TopBarHarness>
        </TooltipProvider>
      </QueryClientProvider>,
    ),
    router,
  };
}

/** The app's top bar shows what the page sets (TopBarSlot); the harness shows the actions the same way. */
function TopBarHarness({ children }: { children: ReactNode }) {
  const [bar, setBar] = useState<TopBarState | null>(null);
  return (
    <TopBarSetContext.Provider value={setBar}>
      <header>{bar?.actions}</header>
      {children}
    </TopBarSetContext.Provider>
  );
}

/** The last filter the page asked listOrgRuns for. */
function lastFilter(): RunsFilter {
  return vi.mocked(client.listOrgRuns).mock.calls.at(-1)?.[1] ?? {};
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'], now });
  calls.answer = fromMock;
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.clearAllMocks();
});

describe('the Runs page states', () => {
  it('shows the loading state: the app background, an sr-only Loading and aria-busy', async () => {
    calls.answer = () => new Promise(() => {});
    renderRuns();

    const loading = (await screen.findByText('Loading')).closest('[aria-busy="true"]');
    expect(loading).not.toBeNull();
    expect(screen.queryByRole('table')).toBeNull();
  });

  it('shows the empty state: one sentence and one action', async () => {
    calls.answer = () => emptyList;
    renderRuns();

    expect(await screen.findByRole('heading', { name: 'No runs today yet' })).toBeTruthy();
    expect(screen.getByText('Runs appear here when an agent starts work on an item.')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Go to Home' }).getAttribute('href')).toBe('/o/acme');
  });

  it('shows the error with a Retry that asks again', async () => {
    calls.answer = () => {
      throw new Error('goald is down');
    };
    renderRuns();

    const alert = await screen.findByRole('alert');
    expect(within(alert).getByText('Couldn’t load runs. Check your connection and try again.')).toBeTruthy();

    calls.answer = fromMock;
    fireEvent.click(within(alert).getByRole('button', { name: 'Retry' }));

    expect(await screen.findByRole('table')).toBeTruthy();
  });

  it('shows the ready state with the pictured rows, the KPI tiles and the footer', async () => {
    renderRuns();

    const table = await screen.findByRole('table');
    const row = (title: RegExp) => within(table).getByText(title).closest('tr') as HTMLElement;

    expect(within(row(/Fix flaky TestLeaseExpiry$/)).getByText('Running')).toBeTruthy();
    expect(within(row(/Split the store package by port$/)).getByText('Running')).toBeTruthy();
    expect(within(row(/Check the budget before each run$/)).getByText('Failed')).toBeTruthy();
    expect(within(row(/Checkout API p95 over 900 ms$/)).getByText('Stopped')).toBeTruthy();
    expect(within(row(/Refund for a double charge$/)).getByText('Finished')).toBeTruthy();
    expect(screen.getByText('Hit the 250-turn cap while rerunning task check:changed after each edit.')).toBeTruthy();

    for (const name of [
      'Factory',
      'Item',
      'Station',
      'Agent',
      'Model',
      'Started',
      'Duration',
      'Turns',
      'Cost',
      'Outcome',
    ]) {
      expect(screen.getByRole('columnheader', { name })).toBeTruthy();
    }
    expect(screen.getByRole('heading', { name: 'Runs', level: 1 })).toBeTruthy();
    expect(screen.getByText('Every time an agent worked on an item, in all factories.')).toBeTruthy();
    expect(screen.getByText('Each one starts over')).toBeTruthy();
    expect(screen.getByText(/\d+ running now/)).toBeTruthy();
    expect(screen.getByText(/^Showing \d+ of \d+ runs today\.$/)).toBeTruthy();
  });

  it('never shows an agent copy number', async () => {
    renderRuns();
    const table = await screen.findByRole('table');

    expect(within(table).queryByText(/(engineer|architect|reviewer|investigator|responder)-\d+$/)).toBeNull();
    expect(within(table).getAllByText('backend-engineer').length).toBeGreaterThan(0);
  });
});

describe('the Runs page filters live in the URL', () => {
  it('reads the factory, outcome, range and search from the URL and passes them to listOrgRuns', async () => {
    renderRuns('?status=failed&factory=sre&range=yesterday&q=checkout');

    await waitFor(() => expect(lastFilter()).toMatchObject({ factory: 'sre', status: 'failed', q: 'checkout' }));
    expect(lastFilter()).toMatchObject({ since: yesterday, until: today, limit: 50 });
    expect((await screen.findByRole('button', { name: /Failed/ })).getAttribute('aria-pressed')).toBe('true');
  });

  it('writes the outcome to the URL and asks for it', async () => {
    const { router } = renderRuns();
    await screen.findByRole('table');

    fireEvent.click(screen.getByRole('button', { name: /^Running/ }));

    expect(router.state.location.search).toMatchObject({ status: 'running' });
    await waitFor(() => expect(lastFilter()).toMatchObject({ status: 'running' }));
  });

  it('writes the search text to the URL once typing pauses', async () => {
    const { router } = renderRuns();
    await screen.findByRole('table');

    const search = screen.getByRole('searchbox', { name: 'Search runs' });
    expect(search.getAttribute('placeholder')).toBe('Item, agent or model');
    fireEvent.change(search, { target: { value: 'budget' } });

    await waitFor(() => expect(router.state.location.search).toMatchObject({ q: 'budget' }));
    await waitFor(() => expect(lastFilter()).toMatchObject({ q: 'budget' }));
  });

  it('reads the factory and range menus into the URL', async () => {
    const { router } = renderRuns();
    await screen.findByRole('table');

    fireEvent.pointerDown(screen.getByRole('button', { name: /All factories/ }), { button: 0, ctrlKey: false });
    fireEvent.click(await screen.findByRole('menuitemradio', { name: 'SRE' }));
    await waitFor(() => expect(router.state.location.search).toMatchObject({ factory: 'sre' }));

    fireEvent.pointerDown(screen.getByRole('button', { name: /Today/ }), { button: 0, ctrlKey: false });
    fireEvent.click(await screen.findByRole('menuitemradio', { name: 'Yesterday' }));
    await waitFor(() => expect(router.state.location.search).toMatchObject({ range: 'yesterday' }));
    await waitFor(() => expect(lastFilter()).toMatchObject({ since: yesterday, until: today }));
  });
});

describe('the controls that are not built yet', () => {
  it('renders Export CSV disabled, focusable, and inert', async () => {
    const { router } = renderRuns();
    await screen.findByRole('table');
    const before = router.state.location.href;
    const calls = vi.mocked(client.listOrgRuns).mock.calls.length;

    const control = screen.getByRole('button', { name: 'Export CSV' });
    expect(control.getAttribute('aria-disabled')).toBe('true');
    expect(control.tabIndex).not.toBe(-1);
    fireEvent.focus(control);
    expect(await screen.findByText('Not available yet')).toBeTruthy();
    fireEvent.click(control);

    expect(router.state.location.href).toBe(before);
    expect(vi.mocked(client.listOrgRuns).mock.calls.length).toBe(calls);
  });

  it('renders the Waiting on you tile and chip disabled, and says why', async () => {
    renderRuns();
    await screen.findByRole('table');

    const controls = screen.getAllByRole('button', { name: /Waiting on you/ });
    expect(controls).toHaveLength(2);
    for (const control of controls) {
      expect(control.getAttribute('aria-disabled')).toBe('true');
      expect(control.tabIndex).not.toBe(-1);
    }
    fireEvent.focus(controls[1] as HTMLElement);
    expect(await screen.findByText('Not available yet')).toBeTruthy();
  });

  it('renders Open transcript and Send to Jonah on a failed row disabled and inert', async () => {
    const { router } = renderRuns();
    const table = await screen.findByRole('table');
    const failed = within(table)
      .getByText(/Check the budget before each run$/)
      .closest('tr') as HTMLElement;
    const detail = failed.nextElementSibling as HTMLElement;
    const before = router.state.location.href;

    for (const name of ['Open transcript', 'Send to Jonah']) {
      const control = within(detail).getByRole('button', { name });
      expect(control.getAttribute('aria-disabled')).toBe('true');
      fireEvent.click(control);
    }
    expect(router.state.location.href).toBe(before);
  });
});
