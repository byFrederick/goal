// @vitest-environment jsdom

import { useSearch } from '@tanstack/react-router';
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { restoreFactory } from '../../api/client';
import { archivedFactoriesQuery, factoriesQuery, inboxQuery, orgQuery, overviewQuery } from '../../api/queries';
import type { Factory, Org, OrgSummary } from '../../api/types';
import { renderOrgPage } from '../../test/route';
import { FactoriesPage } from './factories-page';
import { factoriesSearch } from './search';

// A fake goald: the organization's factories as they are. Restoring moves one to the active list.
const fake = vi.hoisted(() => {
  const store: { active: Factory[]; archived: Factory[] } = { active: [], archived: [] };
  return store;
});

vi.mock(import('../../api/client'), async (original) => ({
  ...(await original()),
  listFactories: vi.fn(async (_org: string, options?: { archived?: boolean }) =>
    options?.archived ? fake.archived : fake.active,
  ),
  restoreFactory: vi.fn(async (_org: string, factory: string) => {
    const restored = fake.archived.find((f) => f.id === factory);
    if (!restored) throw new Error('goald holds no such archived factory');
    fake.archived = fake.archived.filter((f) => f.id !== factory);
    const { archived_at: _archived, archived_by: _by, ...active } = restored;
    fake.active = [...fake.active, active];
    return active;
  }),
}));

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  fake.active = [];
  fake.archived = [];
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: 'owner' };
const archived: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'sre',
  title: 'SRE',
  work_types: ['incident'],
  stations: [],
  current: null,
  draft: null,
  paused: true,
  archived_at: '2026-10-25T09:00:00Z',
  archived_by: { kind: 'user', id: 'local', name: 'Dana' },
  created_at: '2026-10-01T00:00:00Z',
  role: 'editor',
  can: [],
};

const orgWith = (can: string[]): Org => ({ ...org, created_at: '2026-10-01T00:00:00Z', can });

/** The Factories home at ?archived=true, or at its default when `archived` is false; its list seeded. */
function renderHome({
  can,
  archivedList,
  showArchived,
}: {
  can: string[];
  archivedList: Factory[];
  showArchived: boolean;
}) {
  fake.archived = archivedList;
  return renderOrgPage({
    // The route reads its search the same way (factoriesSearch), so the filter's URL is what shows.
    page: () => {
      const { archived } = factoriesSearch(useSearch({ strict: false }));
      return <FactoriesPage archived={archived === true} />;
    },
    org,
    search: showArchived ? { archived: 'true' } : undefined,
    seed: [
      [orgQuery(org.id).queryKey, orgWith(can)],
      [overviewQuery(org.id).queryKey, { factories: [], waiting_on_you: 0, spend_usd: 0, budget_usd: null }],
      [inboxQuery(org.id).queryKey, { entries: [], truncated: false }],
      [factoriesQuery(org.id).queryKey, []],
      [archivedFactoriesQuery(org.id).queryKey, archivedList],
    ],
  });
}

describe('the Archived filter on the Factories home', () => {
  it('lists the archived factories with when and by whom they were archived', async () => {
    renderHome({ can: ['factory:archive'], archivedList: [archived], showArchived: true });

    const link = await screen.findByRole('link', { name: 'SRE' });
    expect(link.getAttribute('href')).toBe('/o/acme/f/sre');
    expect(screen.getByText('Archived Oct 25 by Dana')).toBeTruthy();
  });

  it('is reached from the Factories home, and leaves the active list for the archived one', async () => {
    const { router } = renderHome({ can: ['factory:archive'], archivedList: [archived], showArchived: false });

    expect(screen.queryByRole('link', { name: 'SRE' })).toBeNull();
    fireEvent.click(await screen.findByRole('radio', { name: 'Archived' }));

    await screen.findByRole('link', { name: 'SRE' });
    expect(router.state.location.search).toEqual({ archived: true });
  });

  it('restores a factory: restoreFactory is called and its row leaves the list', async () => {
    fake.active = [];
    renderHome({ can: ['factory:archive'], archivedList: [archived], showArchived: true });

    fireEvent.click(await screen.findByRole('button', { name: 'Restore sre' }));

    await waitFor(() => expect(restoreFactory).toHaveBeenCalledWith('org-1', 'factory-1'));
    await screen.findByText('No archived factories');
    expect(screen.queryByRole('link', { name: 'SRE' })).toBeNull();
  });

  it('offers no Restore without factory:archive', async () => {
    renderHome({ can: [], archivedList: [archived], showArchived: true });

    const row = (await screen.findByRole('link', { name: 'SRE' })).closest('li') as HTMLElement;
    expect(within(row).queryByRole('button')).toBeNull();
  });

  it('says so when nothing is archived', async () => {
    renderHome({ can: ['factory:archive'], archivedList: [], showArchived: true });

    expect(await screen.findByText('No archived factories')).toBeTruthy();
  });
});
