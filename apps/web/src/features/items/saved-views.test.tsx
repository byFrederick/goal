// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError, createSavedView, deleteSavedView, listSavedViews, updateSavedView } from '../../api/client';
import type { Factory, OrgSummary, SavedView } from '../../api/types';
import { renderFactoryPage } from '../../test/route';
import { TooltipProvider } from '../../ui/tooltip';
import { ItemsPage } from './items-page';

// The saved views are the API's: the tests set which views the factory holds and which requests refuse.
// The list of items is empty, so the page's own table does not get in the way.
const api = vi.hoisted(() => ({
  views: [] as SavedView[],
  refuse: null as ApiError | null,
}));

vi.mock(import('../../api/client'), async (original) => ({
  ...(await original()),
  listItems: vi.fn(async () => ({ items: [], next_after: null })),
  listSavedViews: vi.fn(async () => ({ views: api.views })),
  createSavedView: vi.fn(
    async (_org: string, _factory: string, view: { name: string; shared: boolean; filters: unknown }) => {
      if (api.refuse) throw api.refuse;
      return { id: 'view-new', ...view, mine: true, created_at: '', updated_at: '' } as SavedView;
    },
  ),
  updateSavedView: vi.fn(async (_org: string, _factory: string, id: string, change: { name: string }) => {
    if (api.refuse) throw api.refuse;
    return { ...api.views.find((v) => v.id === id), name: change.name } as SavedView;
  }),
  deleteSavedView: vi.fn(async () => {
    if (api.refuse) throw api.refuse;
  }),
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  api.views = [];
  api.refuse = null;
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: null };
const factory: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'sre',
  title: 'SRE',
  work_types: ['incident'],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'approver',
  can: ['item:operate', 'item:decide'],
} as unknown as Factory;

const view = (over: Partial<SavedView>): SavedView => ({
  id: 'view-1',
  name: 'Morning',
  shared: false,
  filters: {},
  mine: true,
  created_at: '2026-10-20T00:00:00Z',
  updated_at: '2026-10-20T00:00:00Z',
  ...over,
});

function renderItems({
  can = factory.can,
  search = {},
}: {
  can?: readonly string[];
  search?: Record<string, string>;
} = {}) {
  return renderFactoryPage({
    page: () => (
      <TooltipProvider>
        <ItemsPage />
      </TooltipProvider>
    ),
    path: 'items/',
    org,
    factory: { ...factory, can: [...can] } as Factory,
    search,
  });
}

/** Opens the "+ Save view" dialog and waits for its name field. */
async function openSaveDialog() {
  fireEvent.click(await screen.findByRole('button', { name: '+ Save view' }));
  return screen.findByRole('dialog', { name: 'Save view' });
}

describe('saving a view from the Items page', () => {
  it('saves the current filters as a personal view', async () => {
    renderItems({ search: { state: 'waiting_human', type: 'incident' } });
    const dialog = await openSaveDialog();
    fireEvent.change(within(dialog).getByRole('textbox', { name: 'Name' }), { target: { value: '  Morning  ' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save view' }));

    await waitFor(() =>
      expect(createSavedView).toHaveBeenCalledWith('org-1', 'factory-1', {
        name: 'Morning',
        shared: false,
        filters: { state: ['waiting_human'], type: ['incident'] },
      }),
    );
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Save view' })).toBeNull());
  });

  it('hides Share with the factory without factory:manage', async () => {
    renderItems({ can: ['factory:read', 'item:operate'] });
    const dialog = await openSaveDialog();
    expect(within(dialog).queryByRole('switch', { name: 'Share with the factory' })).toBeNull();
  });

  it('offers Share with the factory to a person who holds factory:manage, and shares the view', async () => {
    renderItems({ can: ['factory:read', 'factory:manage'] });
    const dialog = await openSaveDialog();
    fireEvent.click(within(dialog).getByRole('switch', { name: 'Share with the factory' }));
    fireEvent.change(within(dialog).getByRole('textbox', { name: 'Name' }), { target: { value: 'Team' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save view' }));
    await waitFor(() =>
      expect(createSavedView).toHaveBeenCalledWith('org-1', 'factory-1', { name: 'Team', shared: true, filters: {} }),
    );
  });

  it('keeps the dialog open and names the problem when the name is taken', async () => {
    api.refuse = new ApiError({
      title: 'Conflict',
      status: 409,
      code: 'view-name-taken',
      detail: 'You already have a view named Morning.',
    } as ConstructorParameters<typeof ApiError>[0]);
    renderItems();
    const dialog = await openSaveDialog();
    fireEvent.change(within(dialog).getByRole('textbox', { name: 'Name' }), { target: { value: 'Morning' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save view' }));

    expect(await within(dialog).findByText('You already have a view named Morning.')).toBeTruthy();
    expect(screen.getByRole('dialog', { name: 'Save view' })).toBeTruthy();
  });
});

describe('the saved views nav', () => {
  it('lists shared views, then the caller’s own, after the built-in tabs', async () => {
    api.views = [
      view({ id: 'v-own', name: 'Mine first', shared: false, mine: true }),
      view({ id: 'v-team', name: 'Team', shared: true, mine: false }),
    ];
    renderItems();
    const nav = await screen.findByRole('navigation', { name: 'Saved views' });
    await waitFor(() => expect(listSavedViews).toHaveBeenCalled());
    const names = within(nav)
      .getAllByRole('link')
      .map((link) => link.textContent?.replace(/\d+$/, ''));
    expect(names).toEqual(['Open', 'Waiting on you', 'Expedite', 'Done this week', 'Team', 'Mine first']);
  });

  it('marks a saved view current when the URL has its filters', async () => {
    api.views = [view({ id: 'v-team', name: 'Team', shared: true, mine: false, filters: { lane: ['fast'] } })];
    renderItems({ search: { lane: 'fast' } });
    const link = await screen.findByRole('link', { name: 'Team' });
    expect(link.getAttribute('aria-current')).toBe('page');
  });

  it('shows Try again when the views fail to load, and the built-in tabs still render', async () => {
    vi.mocked(listSavedViews).mockRejectedValueOnce(new Error('The server is busy'));
    renderItems();
    expect(await screen.findByRole('alert')).toBeTruthy();
    expect(screen.getByRole('link', { name: /^Open/ })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeTruthy();
  });
});

describe('renaming and deleting a view', () => {
  it('renames a view and keeps its filters', async () => {
    api.views = [view({ id: 'v-own', name: 'Morning', filters: { state: ['waiting_human'] } })];
    renderItems();
    fireEvent.keyDown(await screen.findByRole('button', { name: 'Actions for Morning' }), { key: 'Enter' });
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Rename…' }));
    const dialog = await screen.findByRole('dialog', { name: 'Rename view' });
    fireEvent.change(within(dialog).getByRole('textbox', { name: 'Name' }), { target: { value: 'Early' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Rename' }));

    await waitFor(() =>
      expect(updateSavedView).toHaveBeenCalledWith('org-1', 'factory-1', 'v-own', {
        name: 'Early',
        filters: { state: ['waiting_human'] },
      }),
    );
  });

  it('deletes a view after the person confirms', async () => {
    api.views = [view({ id: 'v-own', name: 'Morning' })];
    renderItems();
    fireEvent.keyDown(await screen.findByRole('button', { name: 'Actions for Morning' }), { key: 'Enter' });
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete…' }));
    const dialog = await screen.findByRole('dialog', { name: /^Delete/ });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete' }));

    await waitFor(() => expect(deleteSavedView).toHaveBeenCalledWith('org-1', 'factory-1', 'v-own'));
  });

  it('shows the problem in the dialog when a view is not found', async () => {
    api.views = [view({ id: 'v-own', name: 'Morning' })];
    api.refuse = new ApiError({
      title: 'Not Found',
      status: 404,
      code: 'view-not-found',
      detail: 'That view is gone.',
    } as ConstructorParameters<typeof ApiError>[0]);
    renderItems();
    fireEvent.keyDown(await screen.findByRole('button', { name: 'Actions for Morning' }), { key: 'Enter' });
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete…' }));
    const dialog = await screen.findByRole('dialog', { name: /^Delete/ });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete' }));
    expect(await within(dialog).findByText('That view is gone.')).toBeTruthy();
  });
});
