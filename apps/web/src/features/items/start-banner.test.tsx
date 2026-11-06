// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { clearItemStart, setItemStart } from '../../api/client';
import type { Factory, Item, ItemEvent } from '../../api/types';
import { expectBusyOnClick } from '../../test/busy';
import { renderFactoryPage } from '../../test/route';
import { ClosedBanner, closedBy, StartBanner } from './start-banner';

vi.mock('../../api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/client')>()),
  clearItemStart: vi.fn(),
  setItemStart: vi.fn(),
}));

afterEach(() => {
  cleanup();
  vi.mocked(clearItemStart).mockReset();
  vi.mocked(setItemStart).mockReset();
});

const org = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: null };
const factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'software',
  work_types: [],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'approver',
  can: ['item:operate'],
} as unknown as Factory;

const later = {
  id: 'item-561',
  ref: 'software#561',
  title: 'Add the migration',
  state: 'queued',
  start: { at: '2026-11-19T09:00:00' },
} as Item;

const render = (page: () => React.ReactNode) => renderFactoryPage({ page, path: 'items/', org, factory });

describe('StartBanner', () => {
  it('says when the item starts and offers Start now and Change', async () => {
    render(() => <StartBanner item={later} />);
    const banner = await screen.findByRole('status');

    expect(banner.textContent).toContain('Starts Thu, Nov 19 at 09:00.');
    expect(within(banner).getByRole('button', { name: /^Start now/ })).toBeTruthy();
    expect(within(banner).getByRole('button', { name: /^Change/ })).toBeTruthy();
  });

  it('starts the item now through clearItemStart', async () => {
    vi.mocked(clearItemStart).mockResolvedValue({} as Item);
    render(() => <StartBanner item={later} />);

    fireEvent.click(await screen.findByRole('button', { name: /^Start now/ }));

    await waitFor(() => expect(clearItemStart).toHaveBeenCalledWith('org-1', 'item-561'));
    expect(await screen.findByText('Started')).toBeTruthy();
  });

  it('changes the start through setItemStart', async () => {
    vi.mocked(setItemStart).mockResolvedValue({} as Item);
    render(() => <StartBanner item={later} />);

    fireEvent.click(await screen.findByRole('button', { name: /^Change/ }));
    const dialog = await screen.findByRole('dialog');
    fireEvent.change(within(dialog).getByLabelText('Date'), { target: { value: '2026-11-24' } });
    fireEvent.change(within(dialog).getByLabelText('Time'), { target: { value: '10:15' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(setItemStart).toHaveBeenCalledWith('org-1', 'item-561', { at: '2026-11-24T10:15' }));
  });

  it('shows nothing for an item that may start now or is closed', async () => {
    render(() => <StartBanner item={{ ...later, start: undefined as never }} />);
    render(() => <StartBanner item={{ ...later, state: 'cancelled' }} />);
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull());
  });
});

describe('Closed by', () => {
  const cancelled = (data: Record<string, unknown>): ItemEvent =>
    ({
      id: 9,
      item_id: 'item-1',
      at: '2026-11-01T10:00:00Z',
      kind: 'cancelled',
      station: 'spec',
      actor: 'agent:architect',
      data,
    }) as ItemEvent;

  it('reads the agent and reason of an item an agent closed', () => {
    expect(
      closedBy({ state: 'cancelled' }, [
        cancelled({ closed: true, agent: 'architect', reason: 'Nothing here is work.' }),
      ]),
    ).toEqual({ agent: 'architect', reason: 'Nothing here is work.' });
  });

  it('says nothing for a person’s cancel or an open item', () => {
    expect(closedBy({ state: 'cancelled' }, [cancelled({ reason: 'No longer needed' })])).toBeNull();
    expect(closedBy({ state: 'queued' }, [cancelled({ closed: true, agent: 'architect' })])).toBeNull();
  });

  it('shows "Closed by <agent>" with the reason', async () => {
    render(() => <ClosedBanner closed={{ agent: 'architect', reason: 'Nothing here is work.' }} />);
    const banner = await screen.findByRole('status');
    expect(banner.textContent).toContain('Closed by architect.');
    expect(banner.textContent).toContain('Nothing here is work.');
  });

  it('shows Save as busy while the new start is pending and sends it once', async () => {
    vi.mocked(setItemStart).mockImplementation(() => new Promise(() => {}));
    render(() => <StartBanner item={later} />);

    fireEvent.click(await screen.findByRole('button', { name: /^Change/ }));
    const dialog = await screen.findByRole('dialog');
    fireEvent.change(within(dialog).getByLabelText('Date'), { target: { value: '2026-11-24' } });
    fireEvent.change(within(dialog).getByLabelText('Time'), { target: { value: '10:15' } });
    await expectBusyOnClick(within(dialog).getByRole('button', { name: 'Save' }), 'Saving…');

    expect(setItemStart).toHaveBeenCalledTimes(1);
  });
});
