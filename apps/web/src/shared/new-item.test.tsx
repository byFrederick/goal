// @vitest-environment jsdom
import { QueryClient, QueryClientProvider, queryOptions } from '@tanstack/react-query';
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
import { factoriesQuery } from '../api/queries';
import type { Factory, OrgSummary } from '../api/types';
import { TooltipProvider } from '../ui/tooltip';
import { NewItemFromSidebar } from './new-item';

// The factories list comes from a source the test sets, so each state (pending, failing, ready) is
// exact and no request leaves the test.
const source = vi.hoisted(() => ({ load: (): Promise<unknown[]> => Promise.resolve([]) }));
vi.mock('../api/queries', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api/queries')>();
  return {
    ...actual,
    factoriesQuery: (org: string) =>
      queryOptions({ queryKey: ['test', 'factories', org], queryFn: () => source.load() as Promise<never> }),
  };
});

const summary: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'Ac', factory_count: 3, role: 'owner' };

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
    can: [],
    ...extra,
  }) as Factory;

const writable = [
  factory('sre', { can: ['item:create'] }),
  factory('goal', { can: ['item:create'] }),
  factory('support'),
];

/** New item in the sidebar under the organization's route, with the factories list in `list`. */
function renderPicker(list: Factory[] | 'pending' | 'error', at = '/o/acme') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  if (list === 'pending') source.load = () => new Promise(() => {});
  else if (list === 'error') source.load = () => Promise.reject(new TypeError('Failed to load'));
  else source.load = () => Promise.resolve(list);

  const rootRoute = createRootRoute();
  const orgRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/o/$org',
    beforeLoad: () => ({ org: summary }),
    component: () => (
      <>
        <NewItemFromSidebar />
        <Outlet />
      </>
    ),
  });
  const home = createRoute({ getParentRoute: () => orgRoute, path: '/', component: () => <p>Home page</p> });
  const factoryRoute = createRoute({ getParentRoute: () => orgRoute, path: '/f/$factory', component: Outlet });
  const floor = createRoute({ getParentRoute: () => factoryRoute, path: '/', component: () => <p>Floor page</p> });
  const router = createRouter({
    routeTree: rootRoute.addChildren([orgRoute.addChildren([home, factoryRoute.addChildren([floor])])]),
    history: createMemoryHistory({ initialEntries: [at] }),
  });
  render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <RouterProvider router={router} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return { client };
}

const newItemButton = () => screen.findByRole('button', { name: /New item/ });

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe('the sidebar’s New item picker', () => {
  it('lists only the factories the person may add items to, sorted by name', async () => {
    renderPicker(writable);
    fireEvent.click(await newItemButton());

    const picker = await screen.findByRole('combobox', { name: 'Factory' });
    expect(
      within(picker)
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual(['Choose a factory', 'goal', 'sre']);
  });

  it('renders loading as busy and disabled, with the picker saying Loading', async () => {
    renderPicker('pending');
    fireEvent.click(await newItemButton());

    const picker = await screen.findByRole('combobox', { name: 'Factory' });
    expect(picker.getAttribute('aria-busy')).toBe('true');
    expect((picker as HTMLSelectElement).disabled).toBe(true);
    expect(within(picker).getByRole('option', { name: 'Loading…' })).toBeTruthy();
  });

  it('renders an error with its message and a Retry that asks again', async () => {
    renderPicker('error');
    fireEvent.click(await newItemButton());

    expect(await screen.findByRole('alert')).toBeTruthy();
    expect(screen.getByText('Could not load your factories.')).toBeTruthy();
    const load = vi.fn(source.load);
    source.load = load;
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await waitFor(() => expect(load).toHaveBeenCalledTimes(1));
  });

  it('renders empty when the list empties while the composer is open', async () => {
    const { client } = renderPicker(writable);
    fireEvent.click(await newItemButton());
    await screen.findByRole('combobox', { name: 'Factory' });

    client.setQueryData(factoriesQuery(summary.id).queryKey, []);
    expect(await screen.findByText('No factory takes new items from you.')).toBeTruthy();
    expect(screen.queryByRole('combobox', { name: 'Factory' })).toBeNull();
  });

  it('hides New item when none of the factories takes items from the person', async () => {
    renderPicker([factory('support')]);
    await waitFor(() => expect(screen.queryByRole('button', { name: /New item/ })).toBeNull());
  });

  it('asks the person to choose a factory before it creates an item', async () => {
    renderPicker(writable);
    fireEvent.click(await newItemButton());
    fireEvent.change(await screen.findByRole('textbox', { name: 'Title' }), {
      target: { value: 'Check the budget' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Create item' }));

    expect(screen.getByText('Choose a factory for the item.')).toBeTruthy();
    expect(screen.getByRole('combobox', { name: 'Factory' }).getAttribute('aria-invalid')).toBe('true');
  });
});
