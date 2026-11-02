// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router';
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { factoriesQuery, meQuery, orgQuery, orgsQuery } from '../api/queries';
import type { Me, OrgSummary } from '../api/types';
import { TooltipProvider } from '../ui/tooltip';
import { Route } from './o.$org';

// An unknown slug makes the route ask goald for the organizations again; it answers with the same list.
vi.mock('../api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api/client')>()),
  listOrgs: vi.fn(async () => [goal]),
}));

const goal: OrgSummary = { id: 'org-goal', slug: 'goal', name: 'Goal', mark: 'Go', factory_count: 0, role: 'owner' };
const signedIn = {
  kind: 'user',
  mode: 'session',
  platform_admin: false,
  user: { id: 'user-1', name: 'Freddy Cruz', email: 'freddy@example.com' },
} as Me;

class FakeEventSource {
  onopen = null;
  onerror = null;
  onmessage = null;
  addEventListener() {}
  removeEventListener() {}
  close() {}
}

beforeEach(() => {
  vi.stubGlobal('EventSource', FakeEventSource);
  window.scrollTo = () => {};
  Element.prototype.scrollIntoView = () => {};
  window.matchMedia ??= ((query: string) => ({
    matches: false,
    media: query,
    addEventListener() {},
    removeEventListener() {},
    addListener() {},
    removeListener() {},
    onchange: null,
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

/** The real /o/$org route (its beforeLoad, loader, layout and not-found), opened at `url`, nothing fetched. */
function open(url: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  queryClient.setQueryData(orgsQuery.queryKey, [goal]);
  queryClient.setQueryData(factoriesQuery(goal.id).queryKey, []);
  queryClient.setQueryData(orgQuery(goal.id).queryKey, { ...goal, created_at: '2026-10-01T00:00:00Z', can: [] });
  queryClient.setQueryData(meQuery.queryKey, signedIn);
  const root = createRootRoute();
  const org = createRoute({ ...Route.options, getParentRoute: () => root, path: '/o/$org' } as never);
  const router = createRouter({
    routeTree: root.addChildren([org]),
    history: createMemoryHistory({ initialEntries: [url] }),
    context: { queryClient },
  } as never);
  render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <RouterProvider router={router} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

describe('/o/$org not found', () => {
  it('says No such page inside the shell for an unknown page of a known organization', async () => {
    open('/o/goal/nope');

    expect(await screen.findByRole('heading', { level: 1, name: 'No such page' })).toBeTruthy();
    expect(screen.getAllByText('Goal').length).toBeGreaterThan(0);
    expect(screen.getAllByRole('navigation').length).toBeGreaterThan(0);
    expect(document.querySelectorAll('#main')).toHaveLength(1);
    expect(document.querySelector('[aria-current="page"]')?.textContent).toBe('Not found');
  });

  it('says No such organization, without the shell, for an unknown slug', async () => {
    open('/o/nobody/anything');

    expect(await screen.findByRole('heading', { level: 1, name: 'No such organization' })).toBeTruthy();
    expect(document.querySelectorAll('#main')).toHaveLength(1);
    expect(screen.queryByRole('navigation')).toBeNull();
  });
});
