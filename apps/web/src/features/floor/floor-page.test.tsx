// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import {
  boardQuery,
  constraintQuery,
  inboxQuery,
  leadTimeQuery,
  metricsQuery,
  overviewQuery,
  touchesQuery,
  weeklyQuery,
} from '../../api/queries';
import type {
  Board,
  ConstraintReport,
  Factory,
  FactoryOverview,
  FactoryOverviewList,
  InboxEntry,
  ItemCard,
  LeadTimeBridge,
  Metrics,
  MetricsSummary,
  OrgSummary,
  TouchMetrics,
  WeeklyThroughput,
} from '../../api/types';
import { renderFactoryPage } from '../../test/route';
import { TooltipProvider } from '../../ui/tooltip';
import { FloorPage } from './floor-page';
import { addDays, previousRange, rangeOf, todayUtc } from './range';

afterEach(cleanup);

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: null };
const factory: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'sre',
  title: 'SRE',
  work_types: [],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'approver',
  can: ['item:operate', 'item:decide'],
};

const item = (over: Partial<ItemCard>): ItemCard => ({
  id: 'item-3',
  ref: 'sre#3',
  number: 3,
  title: 'Ship the retry fix',
  type: 'task',
  station: 'merge',
  state: 'queued',
  class_of_service: 'standard',
  rework_count: 0,
  cost_usd: 0,
  created_at: '2026-10-25T08:00:00Z',
  updated_at: '2026-10-25T08:00:00Z',
  age_seconds: 600,
  agent: null,
  pr_url: null,
  pull_requests: [],
  slot: null,
  turn: null,
  summary: null,
  in_buffer: false,
  rope_held: false,
  touch_count: 0,
  ...over,
});

const ship: InboxEntry = {
  id: 'decision:item-3',
  kind: 'decision',
  factory: { id: factory.id, name: factory.name, title: factory.title ?? null },
  since: '2026-10-25T09:00:00Z',
  station: 'merge',
  action: 'merge',
  item: item({}),
  approval: null,
  blocked: null,
  andon: null,
  rework: null,
};

const board: Board = {
  factory: { id: factory.id, name: factory.name, title: 'SRE' },
  stations: [],
  assemblies: [],
  constraint: null,
  constraint_source: 'configured',
  line: {
    status: 'running',
    andon: null,
    wip: { in_progress: 0, limit: 0 },
    release: { policy: 'push', enforced: false, drum: null, buffer: null, in_buffer: 0 },
    expedite: null,
    budget: null,
    touches: { finished: 0, per_item_mean: 0, per_item_median: 0, touchless_rate: 0 },
  },
};

const metrics: Metrics = {
  window: '7d',
  from: '2026-10-20',
  to: '2026-10-26',
  type: null,
  throughput: [],
  throughput_per_day: 0,
  lead_time_hours: null,
  assembly_lead_time_hours: null,
  wip: 0,
  rework_rate: 0,
  spend_usd: 0,
  items_done: 0,
  previous: null,
  targets: { throughput_per_day: null, lead_time_hours: null, expedite_lead_time_minutes: null },
  long_waits: null,
};

const weekly: WeeklyThroughput = {
  to: '2026-10-26',
  type: null,
  targets: { throughput_per_day: null, lead_time_hours: null, expedite_lead_time_minutes: null },
  weeks: [],
};

const bridge: LeadTimeBridge = {
  from: '2026-10-20',
  to: '2026-10-26',
  type: null,
  status: 'not_enough_data',
  target_hours: null,
  actual_p85_hours: null,
  gap_hours: null,
  items_done: 0,
  over_target_items: 0,
  steps: [],
  constraint: null,
};

const constraint: ConstraintReport = {
  from: '2026-10-20',
  to: '2026-10-26',
  coverage_since: null,
  configured: null,
  constraint: null,
  source: 'detected',
  candidates: [],
};

const touches: TouchMetrics = {
  from: '2026-10-20',
  to: '2026-10-26',
  type: null,
  version: null,
  weekly: [],
  daily: [],
  stats: {
    finished: 0,
    touches: 0,
    planned: 0,
    unplanned: 0,
    touchless_rate: 0,
    per_item_mean: 0,
    per_item_median: 0,
    planned_per_item: 0,
    unplanned_per_item: 0,
    by_station: [],
    by_kind: [],
    by_reason: [],
    by_version: [],
    line_stops: 0,
  },
};

/** The Floor's flow metrics of the range before, for the deltas: last week's figures. */
const before: MetricsSummary = {
  from: '2026-10-13',
  to: '2026-10-19',
  items_done: 9,
  throughput_per_day: 8.2,
  lead_time_hours: { p50: 20, p85: 35 },
  assembly_lead_time_hours: null,
  rework_rate: 0.12,
};

/** The week's figures the header and tiles read: 7.6 a day against a target of 10, 31 h p85, 14% sent back. */
const flow: Metrics = {
  ...metrics,
  throughput: [
    { day: '2026-10-20', done: 8 },
    { day: '2026-10-21', done: 7 },
  ],
  throughput_per_day: 7.6,
  lead_time_hours: { p50: 18, p85: 31 },
  rework_rate: 0.14,
  spend_usd: 31,
  items_done: 10,
  previous: before,
  targets: { throughput_per_day: 10, lead_time_hours: 48, expedite_lead_time_minutes: null },
};

/** The factory's row of the overview for a window: its spend and the day's or week's budget. */
function overviewRow(spend: number, budget: number | null): FactoryOverview {
  return {
    factory,
    health: 'running',
    andon: null,
    throughput: { per_day: 0, previous_per_day: 0, days: [{ day: '2026-10-26', done: 7 }] },
    constraint: null,
    waiting: { on_you: 0, items_by_type: [], tool_approvals: [] },
    spend_usd: spend,
    budget_usd: budget,
    touches: {
      current: { finished: 0, per_item_mean: 0, per_item_median: 0, touchless_rate: 0 },
      previous: { finished: 0, per_item_mean: 0, per_item_median: 0, touchless_rate: 0 },
    },
  };
}

function overviewList(window: string, factories: FactoryOverview[]): FactoryOverviewList {
  return {
    window,
    since: '2026-10-20',
    until: '2026-10-26',
    waiting_on_you: 0,
    spend_usd: 0,
    budget_usd: null,
    factories,
  };
}

/** The Floor at its default range (the last seven days), with every query it reads seeded. */
function renderFloor({
  entries = [],
  line = board,
  flowMetrics = metrics,
  today = overviewList('1d', [overviewRow(96, 150)]),
  week = overviewList('7d', [overviewRow(412, 1050)]),
}: {
  entries?: InboxEntry[];
  line?: Board;
  flowMetrics?: Metrics;
  today?: FactoryOverviewList;
  week?: FactoryOverviewList;
} = {}) {
  const range = rangeOf({});
  return renderFactoryPage({
    // The app provides the tooltips at its root; the Hands-off tile's tip needs them here too.
    page: () => (
      <TooltipProvider>
        <FloorPage />
      </TooltipProvider>
    ),
    path: '/',
    org,
    factory,
    seed: [
      [boardQuery(org.id, factory.id).queryKey, line],
      [metricsQuery(org.id, factory.id, range).queryKey, flowMetrics],
      [weeklyQuery(org.id, factory.id, range).queryKey, weekly],
      [leadTimeQuery(org.id, factory.id, range).queryKey, bridge],
      [constraintQuery(org.id, factory.id, range).queryKey, constraint],
      [touchesQuery(org.id, factory.id, range).queryKey, touches],
      [touchesQuery(org.id, factory.id, previousRange(range)).queryKey, touches],
      [inboxQuery(org.id, factory.id).queryKey, { entries, truncated: false }],
      [overviewQuery(org.id, '1d').queryKey, today],
      [overviewQuery(org.id, '7d').queryKey, week],
    ],
  });
}

describe('FloorPage', () => {
  it('the header Needs you link counts what waits on this factory and leads to its page', async () => {
    renderFloor({ entries: [ship] });

    const link = await screen.findByRole('link', { name: /^Needs you/ });
    expect(link.getAttribute('href')).toBe('/o/acme/needs-you?factory=sre');
    expect(link.textContent).toContain('1');
  });

  it('has no Needs you card, with or without waiting entries', async () => {
    renderFloor({ entries: [ship] });

    expect(await screen.findByRole('heading', { level: 1, name: 'Floor' })).toBeTruthy();
    expect(screen.queryByRole('region', { name: 'Needs you' })).toBeNull();
  });

  it('shows the line running and the range it covers, against the 7 days before', async () => {
    renderFloor();

    expect(await screen.findByText('Line running')).toBeTruthy();
    expect(screen.getByText('Oct 20 to 26, compared with the 7 days before')).toBeTruthy();
  });

  it('shows the andon state in place of the running chip while an andon pull is open', async () => {
    const stopped: Board = {
      ...board,
      line: {
        ...board.line,
        status: 'stopped',
        andon: {
          id: 1,
          kind: 'manual',
          station: null,
          reason: 'Answers cite a retired runbook',
          pulled_by: 'user:local',
          pulled_at: '2026-10-26T09:00:00Z',
          cleared_by: null,
          cleared_at: null,
          note: null,
          data: {},
        },
      },
    };
    renderFloor({ line: stopped });

    expect(await screen.findByText(/Line stopped/)).toBeTruthy();
    expect(screen.queryByText('Line running')).toBeNull();
  });

  it('writes the 30-day window to the URL and presses it', async () => {
    const { router } = renderFloor();

    fireEvent.click(await screen.findByRole('radio', { name: '30 days' }));
    await waitFor(() => expect(router.state.location.search).toEqual({ from: addDays(todayUtc(), -29) }));
  });

  it('shows the flow figures against their targets, with the change since last week', async () => {
    renderFloor({ flowMetrics: flow });

    const tiles = within(await screen.findByRole('region', { name: 'Flow against targets' }));
    expect(tiles.getByRole('meter', { name: '76% of the target of 10 a day' })).toBeTruthy();
    expect(tiles.getByRole('meter', { name: '65% of the 48 hour target' })).toBeTruthy();
    expect(tiles.getByText('Target 10')).toBeTruthy();
    expect(tiles.getByText('Target 48 h')).toBeTruthy();
    expect(tiles.getByText(/−0\.6/)).toBeTruthy();
    expect(tiles.getByText('No target set')).toBeTruthy();
  });

  it('shows cost per item from the spend over the finished items', async () => {
    renderFloor({ flowMetrics: flow });

    expect(await screen.findByText('$3.10')).toBeTruthy();
  });

  it('spend today against its budget and the week beside it', async () => {
    renderFloor({ flowMetrics: flow });

    expect(await screen.findByRole('meter', { name: "64% of today's budget" })).toBeTruthy();
    expect(screen.getByText(/^\$96/, { selector: 'p' })).toBeTruthy();
    expect(screen.getByText('of $150')).toBeTruthy();
    expect(screen.getByText('Week $412')).toBeTruthy();
  });

  it('hands-off is disabled and shows no number, as it is not built yet', async () => {
    renderFloor({ flowMetrics: flow });

    const handsOff = await screen.findByRole('button', { name: 'Hands-off' });
    expect(handsOff.getAttribute('aria-disabled')).toBe('true');
    expect(within(handsOff).getByText('Not built yet')).toBeTruthy();
  });

  it('spend today shows not enough data when the factory has no overview row', async () => {
    renderFloor({ flowMetrics: flow, today: overviewList('1d', []) });

    expect(await screen.findByText('Not enough data')).toBeTruthy();
  });
});
