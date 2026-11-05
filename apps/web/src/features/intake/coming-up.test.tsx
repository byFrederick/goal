// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { clearItemStart } from '../../api/client';
import type { Factory, IntakeComingUp, OrgSummary } from '../../api/types';
import { renderFactoryPage } from '../../test/route';
import { ComingUp } from './coming-up';

vi.mock('../../api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/client')>()),
  clearItemStart: vi.fn(),
}));

afterEach(() => {
  cleanup();
  vi.mocked(clearItemStart).mockReset();
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: null };
const factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'widgets',
  work_types: [],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'owner',
  can: ['item:operate'],
} as unknown as Factory;

const rows: IntakeComingUp[] = [
  {
    id: 'a',
    ref: 'widgets#1',
    title: 'Later item',
    origin: 'person',
    via: 'cli',
    needs: [],
    start: { at: '2026-11-19T09:00:00Z' },
    created_at: '2026-11-01T09:00:00Z',
  },
  {
    id: 'b',
    ref: 'widgets#2',
    title: 'Held item',
    origin: 'plan',
    needs: ['widgets#1'],
    created_at: '2026-11-01T09:00:00Z',
  },
];

const renderRows = (items = rows) =>
  renderFactoryPage({ page: () => <ComingUp items={items} total={items.length} />, path: 'intake/', org, factory });

describe('Coming up', () => {
  it('starts an item now, shows it started, then takes its row out of the list', async () => {
    vi.mocked(clearItemStart).mockResolvedValue({} as never);
    renderRows();
    const list = await screen.findByRole('region', { name: 'Coming up' });

    fireEvent.click(within(list).getByRole('button', { name: 'Start now: Later item' }));

    await waitFor(() => expect(clearItemStart).toHaveBeenCalledWith('org-1', 'a'));
    expect(await within(list).findByText('Started')).toBeTruthy();
    await waitFor(() => expect(within(list).getByText('Later item').closest('[data-leaving]')).toBeTruthy(), {
      timeout: 3000,
    });
  });

  it('keeps the row and says why when goald refuses', async () => {
    vi.mocked(clearItemStart).mockRejectedValue(new Error('item-started'));
    renderRows();
    const list = await screen.findByRole('region', { name: 'Coming up' });

    fireEvent.click(within(list).getByRole('button', { name: 'Start now: Later item' }));

    await waitFor(() => expect(clearItemStart).toHaveBeenCalled());
    expect(within(list).queryByText('Started')).toBeNull();
    expect(within(list).getByRole('button', { name: 'Start now: Later item' })).toBeTruthy();
  });

  it('says so when nothing is waiting to start', async () => {
    renderRows([]);
    const list = await screen.findByRole('region', { name: 'Coming up' });
    expect(within(list).getByText(/Nothing is waiting to start/)).toBeTruthy();
  });
});
