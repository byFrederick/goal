// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError, archiveFactory, pauseFactory, purgeFactory, restoreFactory } from '../../api/client';
import { defKeys, orgQuery } from '../../api/queries';
import type { Factory, Org, OrgSummary } from '../../api/types';
import { renderFactoryPage } from '../../test/route';
import { FactorySettingsPage } from './settings-page';

// A fake goald: the factory as it is now. The actions change it and the reads answer it.
const fake = vi.hoisted(() => {
  const store: { factory: Factory | undefined } = { factory: undefined };
  const held = (): Factory => {
    if (!store.factory) throw new Error('goald holds no such factory');
    return store.factory;
  };
  const change = (patch: Partial<Factory>): Factory => {
    store.factory = { ...held(), ...patch };
    return store.factory;
  };
  return { store, held, change };
});

vi.mock(import('../../api/client'), async (original) => ({
  ...(await original()),
  pauseFactory: vi.fn(async () => fake.change({ paused: true })),
  resumeFactory: vi.fn(async () => fake.change({ paused: false })),
  archiveFactory: vi.fn(async () => fake.change({ paused: true, archived_at: '2026-10-26T09:00:00Z' })),
  restoreFactory: vi.fn(async () => {
    const { archived_at: _archived, ...restored } = fake.held();
    fake.store.factory = restored;
    return restored;
  }),
  purgeFactory: vi.fn(async () => {
    fake.store.factory = undefined;
  }),
  // Archived factories are left out of the list.
  listFactories: vi.fn(async () => {
    const current = fake.store.factory;
    return current && !current.archived_at ? [current] : [];
  }),
}));

vi.mock(import('../../api/definitions'), async (original) => ({
  ...(await original()),
  getFactory: vi.fn(async () => fake.held()),
}));

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
});

const org: OrgSummary = {
  id: 'org-1',
  slug: 'acme',
  name: 'Acme',
  mark: 'A',
  factory_count: 1,
  role: 'owner',
};
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
  role: 'editor',
  can: ['factory:manage'],
};
const paused: Factory = { ...factory, paused: true };
const archived: Factory = { ...factory, paused: true, archived_at: '2026-10-25T09:00:00Z' };

/** The organization as goald answers it to the caller: its `can` decides what the Danger zone shows. */
const orgWith = (can: string[]): Org => ({ ...org, created_at: '2026-10-01T00:00:00Z', can });

/** Renders the settings page, and waits until it has its factory. */
async function renderSettings(current: Factory, can: string[]) {
  fake.store.factory = current;
  const rendered = renderFactoryPage({
    page: () => <FactorySettingsPage />,
    path: 'settings',
    org,
    factory: current,
    seed: [
      [orgQuery(org.id).queryKey, orgWith(can)],
      [defKeys.factory(org.id, current.id), current],
    ],
  });
  await screen.findByRole('heading', { name: 'Factory settings' });
  return rendered;
}

const archiver = ['factory:archive', 'factory:delete'];

describe('Danger zone of a factory', () => {
  it('offers Pause first for a running factory and pauses then archives', async () => {
    await renderSettings(factory, archiver);

    fireEvent.click(screen.getByRole('button', { name: 'Archive factory…' }));
    const dialog = await screen.findByRole('dialog');
    fireEvent.click(within(dialog).getByRole('button', { name: 'Pause first' }));

    await screen.findByRole('button', { name: 'Restore factory' });
    expect(pauseFactory).toHaveBeenCalledWith('org-1', 'factory-1');
    expect(archiveFactory).toHaveBeenCalledWith('org-1', 'factory-1');
    const pausedAt = vi.mocked(pauseFactory).mock.invocationCallOrder.at(0) ?? 0;
    const archivedAt = vi.mocked(archiveFactory).mock.invocationCallOrder.at(0) ?? 0;
    expect(pausedAt).toBeGreaterThan(0);
    expect(archivedAt).toBeGreaterThan(pausedAt);
  });

  it('shows the busy detail', async () => {
    await renderSettings(paused, archiver);
    vi.mocked(archiveFactory).mockRejectedValueOnce(
      new ApiError({ title: 'Conflict', status: 409, code: 'factory-busy', detail: '2 items are being worked' }),
    );

    fireEvent.click(screen.getByRole('button', { name: 'Archive factory…' }));
    const dialog = await screen.findByRole('dialog');
    fireEvent.click(within(dialog).getByRole('button', { name: 'Archive factory' }));

    const alert = await within(dialog).findByRole('alert');
    expect(alert.textContent).toContain('2 items are being worked');
    expect(pauseFactory).not.toHaveBeenCalled();
  });

  it('shows Restore instead of Archive for an archived factory', async () => {
    await renderSettings(archived, archiver);

    expect(screen.getByRole('button', { name: 'Restore factory' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Archive factory…' })).toBeNull();
    expect(screen.getByText(/It stays paused until you resume it\./)).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Restore factory' }));
    await waitFor(() => expect(restoreFactory).toHaveBeenCalledWith('org-1', 'factory-1'));
    await screen.findByRole('button', { name: 'Archive factory…' });
  });

  it('disables Delete until archived', async () => {
    await renderSettings(paused, archiver);

    const remove = screen.getByRole('button', { name: 'Delete factory…' }) as HTMLButtonElement;
    expect(remove.disabled).toBe(true);
    expect(screen.getByText('Archive the factory first')).toBeTruthy();
  });

  it('deletes only after the typed name and opens the Factories home', async () => {
    const { router } = await renderSettings(archived, archiver);

    fireEvent.click(screen.getByRole('button', { name: 'Delete factory…' }));
    const dialog = await screen.findByRole('dialog');
    const confirm = within(dialog).getByRole('button', { name: 'Delete factory' }) as HTMLButtonElement;
    expect(confirm.disabled).toBe(true);

    fireEvent.change(within(dialog).getByRole('textbox'), { target: { value: 'sre' } });
    expect(confirm.disabled).toBe(false);
    fireEvent.click(confirm);

    await waitFor(() => expect(purgeFactory).toHaveBeenCalledWith('org-1', 'factory-1'));
    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme'));
  });

  it('leaves the Danger zone out without factory:archive', async () => {
    await renderSettings(factory, []);

    expect(screen.queryByText('Danger zone')).toBeNull();
    expect(screen.queryByRole('button', { name: /Archive factory/ })).toBeNull();
  });
});
