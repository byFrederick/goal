// @vitest-environment jsdom

import { ApiError } from '@goal/api-client';
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { toast } from 'sonner';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { getItemTeardown, keepTest } from '../../api/client';
import { builderMetaQuery, keys, teardownQuery } from '../../api/queries';
import type { BuilderMeta, Factory, Teardown } from '../../api/types';
import { renderFactoryPage } from '../../test/route';
import { TeardownTab } from './teardown-tab';

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() }, Toaster: () => null }));
vi.mock(import('../../api/client'), async (original) => ({
  ...(await original()),
  getItemTeardown: vi.fn(),
  keepTest: vi.fn(),
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

const org = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: null };
const factory: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'goal',
  title: 'Goal',
  work_types: ['feature'],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'approver',
  can: [],
};
const meta: BuilderMeta = {
  models: [{ id: 'claude-opus-5-5', label: 'Opus 5.5', default: true }],
  capabilities: { tool_approvals: false },
  limits: { skill_bytes: 1_000_000, skill_files: 50 },
};

const finished: Teardown = {
  open: false,
  lead_time_seconds: 11_400,
  target_seconds: 14_400,
  cost_usd: 3.81,
  budget_usd: 10,
  rework: { loops: 2, seconds: 1_380, cost_usd: 1.48 },
  touch_count: 1,
  touch_wait_seconds: 180,
  stations: [
    { station: 'spec', working_seconds: 360, waiting_seconds: 240, total_seconds: 600 },
    { station: 'build', working_seconds: 1_380, waiting_seconds: 2_280, total_seconds: 3_660 },
  ],
  total: { working_seconds: 1_740, waiting_seconds: 2_520, total_seconds: 4_260 },
  runs: [
    {
      id: 'r1',
      station: 'spec',
      agent: 'spec-writer',
      model: 'claude-opus-5-5',
      turns: 14,
      seconds: 360,
      cost_usd: 0.41,
      status: 'succeeded',
      outcome: 'finished',
    },
    {
      id: 'r2',
      station: 'build',
      agent: 'frontend-engineer',
      model: 'claude-opus-5-5',
      turns: 30,
      seconds: 540,
      cost_usd: 1.1,
      status: 'succeeded',
      outcome: 'check_failed',
    },
    {
      id: 'r3',
      station: 'review',
      agent: 'reviewer',
      model: 'claude-opus-5-5',
      turns: 8,
      seconds: 300,
      cost_usd: 0.38,
      status: 'succeeded',
      outcome: 'merged',
    },
  ],
  run_total: { runs: 3, turns: 52, seconds: 1_200, cost_usd: 1.89 },
  loops: [
    {
      cause: 'check_failed',
      station: 'build',
      reason: 'unit tests failed: 2 of 48',
      at: '2026-10-25T14:16:00Z',
      seconds: 540,
      cost_usd: 1.1,
      event_id: 41,
    },
    {
      cause: 'gate_rejected',
      station: 'review',
      to: 'build',
      reason: 'retries are not capped',
      at: '2026-10-25T15:02:00Z',
      seconds: 840,
      cost_usd: 0.38,
      event_id: 52,
    },
  ],
  touches: [
    {
      touch: {
        event_id: 47,
        kind: 'answered',
        station: 'build',
        at: '2026-10-25T14:41:00Z',
        actor: { kind: 'user', id: 'u1', name: 'Maya Chen' },
        planned: false,
        reason: null,
        reason_by: null,
      },
      wait_seconds: 180,
    },
  ],
};

function renderTab(answer: () => Promise<Teardown>, can: string[] = []) {
  vi.mocked(getItemTeardown).mockImplementation(answer);
  return renderFactoryPage({
    page: () => <TeardownTab number={147} />,
    path: 'items/$ref',
    location: 'items/147?tab=teardown',
    org,
    factory: { ...factory, can },
    seed: [[builderMetaQuery(org.id).queryKey, meta]],
  });
}

describe('the Teardown tab', () => {
  it('renders the breakdown', async () => {
    renderTab(async () => finished);

    expect(await screen.findByText(/Where #147’s time, cost and help went/)).toBeTruthy();
    const totals = screen.getByRole('region', { name: 'Totals' });
    expect(within(totals).getByText('Lead time')).toBeTruthy();
    expect(within(totals).getByText('of a 4 h target')).toBeTruthy();
    expect(within(totals).getByText('of a $10 budget')).toBeTruthy();
    expect(within(totals).getByText('2 loops')).toBeTruthy();
    expect(within(totals).getByText('Human touches')).toBeTruthy();

    const time = screen.getByRole('table', { name: 'Time by station' });
    expect(within(time).getByRole('columnheader', { name: 'Waiting' })).toBeTruthy();
    expect(within(time).getAllByRole('row').at(-1)?.textContent).toContain('Total');

    const runs = screen.getByRole('table', { name: 'Runs' });
    for (const h of ['Station', 'Agent', 'Model', 'Turns', 'Took', 'Cost', 'Outcome']) {
      expect(within(runs).getByRole('columnheader', { name: h })).toBeTruthy();
    }
    expect(within(runs).getByText('Merged')).toBeTruthy();
    expect(within(runs).getByText('Check failed')).toBeTruthy();
    expect(within(runs).getAllByText('Opus 5.5').length).toBe(3);

    expect(screen.getByRole('heading', { name: 'Why it went back' })).toBeTruthy();
    expect(screen.getByText('retries are not capped')).toBeTruthy();
    expect(screen.getByText(/Maya Chen answered/)).toBeTruthy();
    expect(screen.getByText('Worked out from the item’s runs and events each time you open it.')).toBeTruthy();
  });

  it('links each loop to its step in the trace and each touch to the timeline', async () => {
    renderTab(async () => finished);

    await screen.findByText('retries are not capped');
    const traces = screen.getAllByRole('link', { name: 'In the trace' });
    expect(traces.map((a) => a.getAttribute('href'))).toEqual([
      expect.stringMatching(/tab=trace&step=41$/),
      expect.stringMatching(/tab=trace&step=52$/),
    ]);
    const timeline = screen.getByRole('link', { name: 'In the timeline' });
    expect(timeline.getAttribute('href')).not.toContain('tab=');
  });

  it('shows a skeleton while the breakdown loads', () => {
    const { container } = renderTab(() => new Promise<Teardown>(() => {}));

    return waitFor(() => {
      expect(container.querySelector('[aria-busy="true"]')).not.toBeNull();
      expect(screen.getByText('Loading')).toBeTruthy();
    });
  });

  it('writes durations compactly and costs under a cent as <$0.01', async () => {
    renderTab(async () => ({ ...finished, cost_usd: 0.004, lead_time_seconds: 38 * 60 }));
    const totals = await screen.findByRole('region', { name: 'Totals' });
    expect(within(totals).getByText('38 m')).toBeTruthy();
    expect(within(totals).getByText('<$0.01')).toBeTruthy();
  });

  it('gives Runs the full width when nothing went back and no person stepped in', async () => {
    renderTab(async () => ({ ...finished, loops: [], touches: [] }));
    const row = (await screen.findByTestId('runs-row')) as HTMLElement;
    expect(row.className).not.toContain('min-[1180px]');
    expect(row.children.length).toBe(1);
  });

  it('keeps two columns when a side list is present, and Runs a stacked list below 1000 px', async () => {
    renderTab(async () => finished);
    const row = await screen.findByTestId('runs-row');
    expect(row.className).toContain('min-[1180px]:grid-cols');
    expect(row.children.length).toBe(2);
    expect(screen.getByRole('list', { name: 'Runs' }).className).toContain('max-[1000px]:block');
  });

  it('says there is no breakdown yet for an item no agent has picked up', async () => {
    renderTab(async () => ({
      ...finished,
      runs: [],
      loops: [],
      touches: [],
      run_total: { runs: 0, turns: 0, seconds: 0, cost_usd: 0 },
    }));

    expect(await screen.findByRole('heading', { name: 'No breakdown yet' })).toBeTruthy();
  });

  it('shows an error with Try again, which reads it once more', async () => {
    let calls = 0;
    renderTab(async () => {
      calls += 1;
      if (calls === 1) throw new Error('boom');
      return finished;
    });

    fireEvent.click(await screen.findByRole('button', { name: 'Try again' }));
    expect(await screen.findByText(/Where #147’s time/)).toBeTruthy();
    expect(calls).toBe(2);
  });

  it('keys the query under the item, with no polling', () => {
    const q = teardownQuery('org-1', 'goal#147');
    expect(q.queryKey.slice(0, 4)).toEqual(keys.item('org-1', 'goal#147'));
    expect((q as { refetchInterval?: unknown }).refetchInterval).toBeUndefined();
  });
});

describe('Keep as a test', () => {
  it('is offered with definition:edit at a station the item passed the first time, and keeps it', async () => {
    vi.mocked(keepTest).mockResolvedValue({
      id: 't1',
      item: { id: 'i1', number: 147, title: 'Add a retry cap' },
      station: 'spec',
      kept_by: 'user:u1',
      kept_at: '2026-10-26T10:00:00Z',
      last_result: null,
    });
    renderTab(async () => finished, ['definition:edit']);

    fireEvent.click(await screen.findByRole('button', { name: 'Keep as a test' }));
    const dialog = await screen.findByRole('dialog', { name: 'Keep as a test' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Keep as a test' }));

    await waitFor(() =>
      expect(vi.mocked(keepTest).mock.calls[0]?.slice(1)).toEqual(['factory-1', { item: 147, station: 'spec' }]),
    );
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith('Kept #147 as a test at Spec.'));
  });

  it('is not offered without definition:edit', async () => {
    renderTab(async () => finished);
    await screen.findByRole('table', { name: 'Runs' });
    expect(screen.queryByRole('button', { name: 'Keep as a test' })).toBeNull();
  });

  it('says what goald refused', async () => {
    vi.mocked(keepTest).mockRejectedValue(new ApiError({ title: 'Conflict', status: 409, code: 'kept-test-exists' }));
    renderTab(async () => finished, ['definition:edit']);

    fireEvent.click(await screen.findByRole('button', { name: 'Keep as a test' }));
    const dialog = await screen.findByRole('dialog', { name: 'Keep as a test' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Keep as a test' }));
    expect(await within(dialog).findByText('This item is already kept as a test at Spec.')).toBeTruthy();
  });
});
