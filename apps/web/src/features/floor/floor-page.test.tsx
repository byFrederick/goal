// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { approveItem, getFactoryActivity, rejectItem } from '../../api/client';
import {
  activityQuery,
  boardQuery,
  constraintQuery,
  inboxQuery,
  metricsQuery,
  overviewQuery,
  weeklyQuery,
} from '../../api/queries';
import type {
  Activity,
  ActivityEvent,
  Board,
  ConstraintReport,
  Factory,
  FactoryOverview,
  FactoryOverviewList,
  InboxEntry,
  ItemCard,
  Metrics,
  MetricsSummary,
  OrgSummary,
  WeeklyThroughput,
} from '../../api/types';
import { renderFactoryPage } from '../../test/route';
import { TooltipProvider } from '../../ui/tooltip';
import { FloorPage } from './floor-page';
import { addDays, rangeOf, todayUtc } from './range';

// The decisions and the activity feed go through the API client; the other calls are answered by the seeded queries.
vi.mock('../../api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/client')>()),
  approveItem: vi.fn(),
  rejectItem: vi.fn(),
  getFactoryActivity: vi.fn(),
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

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

const constraint: ConstraintReport = {
  from: '2026-10-20',
  to: '2026-10-26',
  coverage_since: null,
  configured: null,
  constraint: null,
  source: 'detected',
  candidates: [],
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

const event = (over: Partial<ActivityEvent>): ActivityEvent => ({
  id: 1,
  item_id: 'item-3',
  at: '2026-10-26T14:32:00Z',
  kind: 'merged',
  station: 'merge',
  actor: 'reviewer',
  data: {},
  ref: 'sre#146',
  number: 146,
  title: 'Ship the retry fix',
  ...over,
});

/** The factory's latest milestone events, newest first: a merge by the reviewer, a move by a frontend engineer copy. */
const feed: Activity = {
  events: [
    event({ id: 2, kind: 'merged', actor: 'reviewer', station: 'review' }),
    event({
      id: 1,
      at: '2026-10-26T13:48:00Z',
      kind: 'moved',
      actor: 'frontend-engineer-2',
      station: 'build',
      ref: 'sre#147',
      number: 147,
    }),
  ],
};

/** The constraint with three busy stations: Build is the constraint at 94%. */
const busy: ConstraintReport = {
  ...constraint,
  coverage_since: '2026-10-01T00:00:00Z',
  configured: 'build',
  constraint: 'build',
  source: 'configured',
  candidates: [
    {
      station: 'spec',
      slots: 2,
      agents: ['architect'],
      busy_ratio: 0.41,
      avg_waiting: 0,
      mean_wait_minutes: null,
      mean_service_minutes: 30,
      leases: 12,
      spend_per_slot_day_usd: null,
      estimate: null,
    },
    {
      station: 'build',
      slots: 6,
      agents: ['frontend-engineer'],
      busy_ratio: 0.94,
      avg_waiting: 3,
      mean_wait_minutes: 40,
      mean_service_minutes: 55,
      leases: 80,
      spend_per_slot_day_usd: null,
      estimate: null,
    },
    {
      station: 'review',
      slots: 2,
      agents: ['reviewer'],
      busy_ratio: 0.58,
      avg_waiting: 0,
      mean_wait_minutes: null,
      mean_service_minutes: 20,
      leases: 30,
      spend_per_slot_day_usd: null,
      estimate: null,
    },
  ],
};

/** Two weeks of merges with a target of 10 a day. */
const twoWeeks: WeeklyThroughput = {
  to: '2026-10-26',
  type: null,
  targets: { throughput_per_day: 10, lead_time_hours: null, expedite_lead_time_minutes: null },
  weeks: [
    { from: '2026-10-13', to: '2026-10-19', done: 57, per_day: 8.1 },
    { from: '2026-10-20', to: '2026-10-26', done: 53, per_day: 7.6 },
  ],
};

const deciding: InboxEntry = {
  ...ship,
  id: 'decision:item-4',
  action: null,
  item: item({ id: 'item-4', ref: 'sre#4', title: 'Add the audit log', station: 'review' }),
  station: 'review',
};

/** The Floor at its default range (the last seven days), with every query it reads seeded. */
function renderFloor({
  entries = [],
  line = board,
  flowMetrics = metrics,
  today = overviewList('1d', [overviewRow(96, 150)]),
  week = overviewList('7d', [overviewRow(412, 1050)]),
  constraintReport = constraint,
  weeks = weekly,
  activity = feed,
}: {
  entries?: InboxEntry[];
  line?: Board;
  flowMetrics?: Metrics;
  today?: FactoryOverviewList;
  week?: FactoryOverviewList;
  constraintReport?: ConstraintReport;
  weeks?: WeeklyThroughput;
  /** null seeds no activity, so the card fetches it (and the test decides the answer). */
  activity?: Activity | null;
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
      [weeklyQuery(org.id, factory.id, range).queryKey, weeks],
      [constraintQuery(org.id, factory.id, range).queryKey, constraintReport],
      [inboxQuery(org.id, factory.id).queryKey, { entries, truncated: false }],
      [overviewQuery(org.id, '1d').queryKey, today],
      [overviewQuery(org.id, '7d').queryKey, week],
      ...(activity ? [[activityQuery(org.id, factory.id).queryKey, activity] as const] : []),
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

  it('shows Needs you, Where work waits, Throughput by week and Activity below the figures, in that order', async () => {
    renderFloor({ entries: [ship] });

    expect(await screen.findByRole('heading', { level: 1, name: 'Floor' })).toBeTruthy();
    const headings = screen.getAllByRole('heading', { level: 2 }).map((h) => h.textContent);
    const from = headings.indexOf('Needs you');
    expect(from).toBeGreaterThanOrEqual(0);
    expect(headings.slice(from, from + 4)).toEqual(['Needs you', 'Where work waits', 'Throughput by week', 'Activity']);
    expect(screen.queryByText('Touches')).toBeNull();
    expect(screen.queryByRole('region', { name: 'Lead time' })).toBeNull();
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

describe('the cards below the figures', () => {
  it('Needs you shows the count and a decision row with Deny and Approve', async () => {
    renderFloor({ entries: [deciding] });

    const card = await screen.findByRole('region', { name: 'Needs you' });
    expect(within(card).getByText('Add the audit log')).toBeTruthy();
    expect(within(card).getByText('sre#4 at Review')).toBeTruthy();
    expect(within(card).getByRole('button', { name: 'Deny' })).toBeTruthy();
    expect(within(card).getByRole('button', { name: 'Approve sre#4' })).toBeTruthy();
  });

  it('Approve calls approveItem with the item and shows no question rows', async () => {
    vi.mocked(approveItem).mockResolvedValue({ ...item({ ref: 'sre#4' }), state: 'queued', station: 'merge' } as never);
    renderFloor({ entries: [deciding] });

    fireEvent.click(await screen.findByRole('button', { name: 'Approve sre#4' }));
    await waitFor(() => expect(approveItem).toHaveBeenCalledWith('org-1', 'sre#4'));
    expect(screen.queryByText(/asked a question/)).toBeNull();
  });

  it('Deny asks for a reason and then calls rejectItem', async () => {
    vi.mocked(rejectItem).mockResolvedValue({ ...item({ ref: 'sre#4' }), state: 'queued', station: 'build' } as never);
    renderFloor({ entries: [deciding] });

    fireEvent.click(await screen.findByRole('button', { name: 'Deny' }));
    const dialog = await screen.findByRole('dialog');
    fireEvent.change(within(dialog).getByRole('textbox'), { target: { value: 'The audit log is missing' } });
    fireEvent.click(within(dialog).getByRole('button', { name: /send back/i }));
    await waitFor(() => expect(rejectItem).toHaveBeenCalledWith('org-1', 'sre#4', 'The audit log is missing'));
  });

  it('a row that is not a decision offers Decide, which opens the item', async () => {
    renderFloor({ entries: [ship] });

    const card = await screen.findByRole('region', { name: 'Needs you' });
    expect(within(card).getByRole('link', { name: 'Decide' }).getAttribute('href')).toContain('/items/sre%233');
    expect(within(card).queryByRole('button', { name: 'Approve sre#3' })).toBeNull();
  });

  it('Needs you says when nothing waits on the person', async () => {
    renderFloor();

    const card = await screen.findByRole('region', { name: 'Needs you' });
    expect(within(card).getByText('Nothing waits on you.')).toBeTruthy();
  });

  it('Where work waits draws one bar per station, the constraint in amber, and names it', async () => {
    renderFloor({ constraintReport: busy });

    const card = await screen.findByRole('region', { name: 'Where work waits' });
    expect(within(card).getByText('Build is the constraint')).toBeTruthy();
    expect(within(card).getByText("Share of the week each station's agents were busy")).toBeTruthy();
    expect(within(card).getByText('94%')).toBeTruthy();
    expect(card.querySelectorAll('.bg-amber')).toHaveLength(1);
  });

  it('the what-if button is not available yet: aria-disabled, and it says so on focus', async () => {
    renderFloor({ constraintReport: busy });

    const card = await screen.findByRole('region', { name: 'Where work waits' });
    const button = within(card).getByRole('button', { name: 'Add a build agent' });
    expect(button.getAttribute('aria-disabled')).toBe('true');
    button.focus();
    expect(await screen.findByText('Not available yet')).toBeTruthy();
    expect(within(card).queryByText(/lead time/i)).toBeNull();
  });

  it('Throughput by week draws each week with the target', async () => {
    renderFloor({ weeks: twoWeeks });

    const card = await screen.findByRole('region', { name: 'Throughput by week' });
    expect(within(card).getByRole('img', { name: /Items merged a day by week/ })).toBeTruthy();
    expect(within(card).getByText('Target 10')).toBeTruthy();
  });

  it('Activity shows Live and a sentence per event, naming the actor by role, never by copy number', async () => {
    renderFloor();

    const card = await screen.findByRole('region', { name: 'Activity' });
    expect(within(card).getByText('Live')).toBeTruthy();
    expect(card.textContent).toContain('sre#146 merged by reviewer at Review');
    expect(card.textContent).toContain('sre#147 moved by frontend-engineer at Build');
    expect(card.textContent).not.toContain('frontend-engineer-2');
  });

  it('Activity says what failed and offers Retry', async () => {
    vi.mocked(getFactoryActivity).mockRejectedValueOnce(new Error('goald is down'));
    renderFloor({ activity: null });

    const card = await screen.findByRole('region', { name: 'Activity' });
    expect(await within(card).findByText('Could not load activity.')).toBeTruthy();
    expect(within(card).getByRole('button', { name: 'Retry' })).toBeTruthy();
  });

  it('Activity is busy until its first answer arrives', async () => {
    vi.mocked(getFactoryActivity).mockReturnValueOnce(new Promise(() => {}));
    renderFloor({ activity: null });

    const card = await screen.findByRole('region', { name: 'Activity' });
    expect(card.getAttribute('aria-busy')).toBe('true');
    expect(within(card).getByText('Loading')).toBeTruthy();
  });
});
