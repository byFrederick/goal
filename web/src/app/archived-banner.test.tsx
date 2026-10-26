// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { restoreFactory } from '../api/client';
import { archivedFactoriesQuery, factoriesQuery, orgQuery } from '../api/queries';
import type { Factory, Org, OrgSummary } from '../api/types';
import { NewItemButton } from '../shared/new-item';
import { renderFactoryPage } from '../test/route';
import { ArchivedBanner } from './archived-banner';

// A fake goald: the factory as it is now. Restore makes it active again, and the lists answer so.
const fake = vi.hoisted(() => {
  const store: { factory: Factory | undefined } = { factory: undefined };
  return store;
});

vi.mock(import('../api/client'), async (original) => ({
  ...(await original()),
  listFactories: vi.fn(async (_org: string, options?: { archived?: boolean }) => {
    const current = fake.factory;
    if (!current) return [];
    return Boolean(current.archived_at) === Boolean(options?.archived) ? [current] : [];
  }),
  restoreFactory: vi.fn(async () => {
    if (!fake.factory) throw new Error('goald holds no such factory');
    const { archived_at: _archived, archived_by: _by, ...restored } = fake.factory;
    fake.factory = restored;
    return restored;
  }),
}));

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  fake.factory = undefined;
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: 'owner' };
const factory: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'sre',
  title: 'SRE',
  work_types: ['incident'],
  stations: [],
  current: null,
  draft: null,
  paused: true,
  created_at: '2026-10-01T00:00:00Z',
  role: 'editor',
  can: ['item:create', 'item:operate', 'item:decide', 'factory:manage'],
};
const archived: Factory = {
  ...factory,
  archived_at: '2026-10-25T09:00:00Z',
  archived_by: { kind: 'user', id: 'local', name: 'Dana' },
};

const orgWith = (can: string[]): Org => ({ ...org, created_at: '2026-10-01T00:00:00Z', can });

/** A page of the factory's, under the banner: the New item control is the write control it must hide. */
function renderPage(current: Factory, can: string[]) {
  fake.factory = current;
  return renderFactoryPage({
    page: () => (
      <>
        <ArchivedBanner />
        <NewItemButton factory={current} />
        <p>The factory’s board</p>
      </>
    ),
    path: 'board',
    org,
    factory: current,
    // Factories as goald lists them: an archived one only in the archived list.
    factories: [],
    seed: [
      [orgQuery(org.id).queryKey, orgWith(can)],
      [factoriesQuery(org.id).queryKey, []],
      [archivedFactoriesQuery(org.id).queryKey, current.archived_at ? [current] : []],
    ],
  });
}

describe('the banner of an archived factory', () => {
  it('says when it was archived and by whom, and that it takes no work', async () => {
    renderPage(archived, ['factory:archive']);

    expect(await screen.findByText('Archived on Oct 25 by Dana. It takes no work.')).toBeTruthy();
    expect(screen.getByText('The factory’s board')).toBeTruthy();
  });

  it('restores the factory, and the banner goes once it is no longer archived', async () => {
    renderPage(archived, ['factory:archive']);

    fireEvent.click(await screen.findByRole('button', { name: 'Restore' }));

    await waitFor(() => expect(restoreFactory).toHaveBeenCalledWith('org-1', 'factory-1'));
    await waitFor(() => expect(screen.queryByText(/It takes no work/)).toBeNull());
  });

  it('offers no Restore without factory:archive', async () => {
    renderPage(archived, []);

    await screen.findByText(/It takes no work/);
    expect(screen.queryByRole('button', { name: 'Restore' })).toBeNull();
  });

  it('does not offer New item on an archived factory', async () => {
    renderPage(archived, ['factory:archive']);

    await screen.findByText(/It takes no work/);
    expect(screen.queryByRole('button', { name: 'New item' })).toBeNull();
  });

  it('offers New item on the same factory while it is active', async () => {
    renderPage(factory, ['factory:archive']);

    expect(await screen.findByRole('button', { name: 'New item' })).toBeTruthy();
    expect(screen.queryByText(/It takes no work/)).toBeNull();
  });
});
