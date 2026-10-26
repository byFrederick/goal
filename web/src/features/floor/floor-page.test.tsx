// @vitest-environment jsdom
import { cleanup, fireEvent, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import {
  boardQuery,
  constraintQuery,
  inboxQuery,
  leadTimeQuery,
  metricsQuery,
  touchesQuery,
  weeklyQuery,
} from '../../api/queries';
import type {
  Board,
  ConstraintReport,
  Factory,
  InboxEntry,
  ItemCard,
  LeadTimeBridge,
  Metrics,
  OrgSummary,
  TouchMetrics,
  WeeklyThroughput,
} from '../../api/types';
import { renderFactoryPage } from '../../test/route';
import { FloorPage } from './floor-page';
import { previousRange, rangeOf } from './range';

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

/** The Floor at its default range (the last seven days), with every query it reads seeded. */
function renderFloor(entries: InboxEntry[] = []) {
  const range = rangeOf({});
  return renderFactoryPage({
    page: FloorPage,
    path: '/',
    org,
    factory,
    seed: [
      [boardQuery(org.id, factory.id).queryKey, board],
      [metricsQuery(org.id, factory.id, range).queryKey, metrics],
      [weeklyQuery(org.id, factory.id, range).queryKey, weekly],
      [leadTimeQuery(org.id, factory.id, range).queryKey, bridge],
      [constraintQuery(org.id, factory.id, range).queryKey, constraint],
      [touchesQuery(org.id, factory.id, range).queryKey, touches],
      [touchesQuery(org.id, factory.id, previousRange(range)).queryKey, touches],
      [inboxQuery(org.id, factory.id).queryKey, { entries, truncated: false }],
    ],
  });
}

describe('FloorPage', () => {
  it('the Inbox button opens the drawer with what waits on this factory', async () => {
    renderFloor([ship]);

    fireEvent.click(await screen.findByRole('button', { name: /^Inbox/ }));
    const drawer = await screen.findByRole('dialog', { name: 'Inbox' });
    expect(within(drawer).getByText('Ready to merge')).toBeTruthy();
    expect(within(drawer).getByRole('button', { name: 'Merge sre#3' })).toBeTruthy();
  });

  it('has no Needs you card, with or without waiting entries', async () => {
    renderFloor([ship]);

    expect(await screen.findByRole('heading', { level: 1, name: 'SRE floor' })).toBeTruthy();
    expect(screen.queryByRole('region', { name: 'Needs you' })).toBeNull();
    expect(screen.queryByText('Needs you')).toBeNull();
  });
});
