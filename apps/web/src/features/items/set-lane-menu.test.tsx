// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { setItemLane } from '../../api/client';
import { versionModelQuery } from '../../api/queries';
import type { Factory, Item, OrgSummary, VersionModel } from '../../api/types';
import type { Movable } from '../../shared/move-menu';
import { renderFactoryPage } from '../../test/route';
import { TooltipProvider } from '../../ui/tooltip';
import { SetLaneDialog } from './set-lane-menu';

// The running version's model is read through getVersionModel: the tests decide what it answers.
const model = vi.hoisted(() => ({
  value: null as unknown,
  fail: false,
  hold: false,
  reads: 0,
}));

vi.mock(import('../../api/definitions'), async (original) => ({
  ...(await original()),
  getVersionModel: vi.fn(async () => {
    model.reads += 1;
    if (model.hold) return new Promise<never>(() => {});
    if (model.fail) throw new Error('The server is busy');
    return { model: model.value, ranges: {} } as VersionModel;
  }),
}));

vi.mock('../../api/client', async (original) => ({
  ...(await original()),
  setItemLane: vi.fn(async (_org: string, key: string, lane: string) => ({ key, lane }) as unknown as Item),
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  model.value = null;
  model.fail = false;
  model.hold = false;
  model.reads = 0;
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

/** A line whose build station routes two lanes, and whose review station routes one of them again. */
const routedLine = {
  line: {
    stations: [
      { id: 'build', kind: 'agent', route: { frontend: 'frontend-engineer', backend: 'backend-engineer' } },
      { id: 'review', kind: 'agent', agent: 'reviewer', route: { frontend: 'frontend-reviewer' } },
    ],
  },
};

const movable = (ref: string): Movable => ({
  key: ref,
  id: ref,
  ref,
  title: `Item ${ref}`,
  station: 'build',
  state: 'queued',
});

function renderMenu(items: Movable[], { seed = true }: { seed?: boolean } = {}) {
  const onOpenChange = vi.fn();
  renderFactoryPage({
    page: () => (
      <TooltipProvider>
        <SetLaneDialog items={items} factory={factory} open onOpenChange={onOpenChange} />
      </TooltipProvider>
    ),
    path: 'items/',
    org,
    factory,
    seed: seed ? [[versionModelQuery(org.id, factory.id, 'current').queryKey, { model: model.value }]] : [],
  });
  return { onOpenChange };
}

describe('Set lane menu', () => {
  it('lists the lanes the stations route by, sorted and once each, with no text field', async () => {
    model.value = routedLine;
    renderMenu([movable('sre#1')], { seed: false });

    const group = await screen.findByRole('group', { name: 'Lane' });
    const lanes = within(group)
      .getAllByRole('radio')
      .map((radio) => radio.getAttribute('value'));
    expect(lanes).toEqual(['backend', 'frontend']);
    expect(screen.queryByRole('textbox')).toBeNull();
  });

  it('shows the loading state while the running version is read', async () => {
    model.hold = true;
    renderMenu([movable('sre#1')], { seed: false });

    const busy = await screen.findByText('Loading');
    expect(busy.parentElement?.getAttribute('aria-busy')).toBe('true');
  });

  it('shows the error and reads the version again on Try again', async () => {
    model.fail = true;
    renderMenu([movable('sre#1')], { seed: false });

    expect(await screen.findByText('The lanes could not be read. Try again.')).toBeTruthy();
    model.fail = false;
    model.value = routedLine;
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));

    expect(await screen.findByRole('radio', { name: 'Backend' })).toBeTruthy();
  });

  it('says that no station routes by lane, and offers no submit', async () => {
    model.value = { line: { stations: [{ id: 'build', kind: 'agent', agent: 'builder' }] } };
    renderMenu([movable('sre#1')], { seed: false });

    expect(await screen.findByText('No station routes items by lane, so there is no lane to set.')).toBeTruthy();
    expect(screen.queryByRole('radio')).toBeNull();
    expect(screen.queryByRole('button', { name: /Set lane to/ })).toBeNull();
  });

  it('sets the chosen lane on each item, one request per item', async () => {
    model.value = routedLine;
    renderMenu([movable('sre#1'), movable('sre#2')]);

    fireEvent.click(await screen.findByRole('radio', { name: 'Frontend' }));
    fireEvent.click(screen.getByRole('button', { name: 'Set lane to Frontend' }));

    await waitFor(() => expect(setItemLane).toHaveBeenCalledTimes(2));
    expect(setItemLane).toHaveBeenCalledWith('org-1', 'sre#1', 'frontend');
    expect(setItemLane).toHaveBeenCalledWith('org-1', 'sre#2', 'frontend');
  });

  it('keeps the submit disabled until a lane is chosen', async () => {
    model.value = routedLine;
    renderMenu([movable('sre#1')]);

    expect(await screen.findByRole('radio', { name: 'Backend' })).toBeTruthy();
    expect((screen.getByRole('button', { name: 'Set lane to …' }) as HTMLButtonElement).disabled).toBe(true);
  });
});
