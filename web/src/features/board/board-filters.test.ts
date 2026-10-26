import { describe, expect, it } from 'vitest';
import type { Board, BoardLane, BoardStation, ItemCard } from '../../api/types';
import { arrangeBoard, assemblyOptions, groupByAssembly, knownView, roleOptions } from './board-filters';

const item = (over: Partial<ItemCard>): ItemCard => ({
  id: 'item-x',
  ref: 'sre#1',
  number: 1,
  title: 'Untitled',
  type: 'task',
  station: 'investigate',
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

const lane = (over: Partial<BoardLane>): BoardLane => ({
  key: 'investigator',
  kind: 'agent',
  role: 'Investigator',
  model: null,
  model_label: null,
  slots: 2,
  busy: 0,
  ...over,
});

const station = (over: Partial<BoardStation>): BoardStation => ({
  id: 'investigate',
  kind: 'agent',
  agents: [],
  wip_limit: null,
  waiting: 0,
  in_progress: 0,
  items: [],
  busy_ratio: null,
  send_back_ratio: null,
  queue_trend: null,
  oldest_waiting_seconds: null,
  done_today: null,
  ...over,
});

const board = (stations: BoardStation[]): Board => ({
  factory: { id: 'factory-1', name: 'sre', title: 'SRE' },
  stations,
  assemblies: [],
  constraint: null,
  constraint_source: 'configured',
  line: {
    status: 'running',
    andon: null,
    wip: { in_progress: 0, limit: null },
    release: { policy: 'push', enforced: false, drum: null, buffer: null, in_buffer: 0 },
    expedite: { limit: null, in_use: 0 },
    budget: null,
    touches: { finished: 0, per_item_mean: 0, per_item_median: 0, touchless_rate: 0 },
  },
});

const partOf = (id: string, title: string, part: number) => ({ id, ref: `sre#${id}`, title, part, parts: 2 });

// Assembly ASM (sre#20) has two parts; the second assembly has one part and no card on the board.
const ASM = 'item-asm';
const asmCard = item({ id: ASM, ref: 'sre#20', title: 'Checkout retry rollout', state: 'assembly' });
const partOne = item({
  id: 'item-21',
  ref: 'sre#21',
  title: 'Add the budget',
  state: 'in_progress',
  agent: 'investigator',
  board_lane: 'investigator',
  assembly: partOf(ASM, 'Checkout retry rollout', 1),
});
const partTwo = item({
  id: 'item-22',
  ref: 'sre#22',
  title: 'Wire the flag',
  board_lane: 'investigator',
  assembly: partOf(ASM, 'Checkout retry rollout', 2),
});
const other = item({
  id: 'item-30',
  ref: 'sre#30',
  title: 'Draft the runbook',
  board_lane: 'investigator',
  assembly: partOf('item-other', 'Docs refresh', 1),
});
const loose = item({ id: 'item-23', ref: 'sre#23', title: 'Triage the export', board_lane: 'triager' });
const ready = item({ id: 'item-24', ref: 'sre#24', title: 'Merge the cleanup', state: 'ready', board_lane: 'triager' });

const twoStations = board([
  station({
    id: 'investigate',
    lanes: [lane({}), lane({ key: 'triager', role: 'Triager' })],
    items: [asmCard, other, partTwo, loose, partOne, ready],
  }),
  station({ id: 'queue', kind: 'queue', items: [] }),
]);

const refs = (b: Board, id = 'investigate') => b.stations.find((s) => s.id === id)?.items.map((it) => it.ref);

describe('assemblyOptions', () => {
  it('lists each assembly once, from its parts and from its own card, by title', () => {
    expect(assemblyOptions(twoStations)).toEqual([
      { id: ASM, title: 'Checkout retry rollout' },
      { id: 'item-other', title: 'Docs refresh' },
    ]);
  });
});

describe('roleOptions', () => {
  it('lists the distinct lane roles of every station, by name', () => {
    expect(roleOptions(twoStations)).toEqual(['Investigator', 'Triager']);
  });
});

describe('knownView', () => {
  it('drops the choices the board does not offer and keeps the rest', () => {
    expect(
      knownView(twoStations, {
        assembly: 'item-gone',
        role: 'Infra engineer',
        status: 'working',
        group: 'assembly',
      }),
    ).toEqual({ assembly: undefined, role: undefined, status: 'working', group: 'assembly' });
  });

  it('drops an unknown status and an unknown group', () => {
    const view = knownView(twoStations, { status: 'blocked' as never, group: 'lane' as never });
    expect(view.status).toBeUndefined();
    expect(view.group).toBeUndefined();
  });
});

describe('arrangeBoard', () => {
  it('keeps every card with no choice made', () => {
    expect(refs(arrangeBoard(twoStations, {}))).toEqual(['sre#20', 'sre#30', 'sre#22', 'sre#23', 'sre#21', 'sre#24']);
  });

  it('keeps an assembly’s own card and its parts, and no other card', () => {
    expect(refs(arrangeBoard(twoStations, { assembly: ASM }))).toEqual(['sre#20', 'sre#22', 'sre#21']);
  });

  it('keeps the cards whose lane has the role', () => {
    expect(refs(arrangeBoard(twoStations, { role: 'Triager' }))).toEqual(['sre#23', 'sre#24']);
  });

  it('keeps the cards whose status is the status icon of their state', () => {
    expect(refs(arrangeBoard(twoStations, { status: 'working' }))).toEqual(['sre#21']);
    expect(refs(arrangeBoard(twoStations, { status: 'waiting' }))).toEqual(['sre#30', 'sre#22', 'sre#23']);
    expect(refs(arrangeBoard(twoStations, { status: 'ready' }))).toEqual(['sre#24']);
    expect(refs(arrangeBoard(twoStations, { status: 'same-files' }))).toEqual([]);
  });

  it('keeps a queued or ready card that waits for the same files under Waiting for same files only', () => {
    const shared = item({
      ref: 'sre#40',
      state: 'queued',
      waiting: { kind: 'files', items: ['sre#3'], files: ['web/src/**'] },
    });
    const starts = item({ ref: 'sre#41', state: 'ready', waiting: { kind: 'after', items: ['sre#3'], files: [] } });
    const shelf = board([station({ id: 'build', items: [shared, starts] })]);
    expect(refs(arrangeBoard(shelf, { status: 'same-files' }), 'build')).toEqual(['sre#40']);
    expect(refs(arrangeBoard(shelf, { status: 'waiting' }), 'build')).toEqual(['sre#41']);
  });

  it('combines the choices: every one must match', () => {
    expect(refs(arrangeBoard(twoStations, { assembly: ASM, status: 'waiting' }))).toEqual(['sre#22']);
  });

  it('keeps the lanes and the numbers of the stations', () => {
    const view = arrangeBoard(twoStations, { role: 'Triager' });
    expect(view.stations[0]?.lanes).toEqual(twoStations.stations[0]?.lanes);
    expect(view.stations[0]?.in_progress).toBe(twoStations.stations[0]?.in_progress);
  });
});

describe('groupByAssembly', () => {
  it('puts each assembly’s cards together in part order, the assemblies in their first card’s place, the rest last', () => {
    // The second assembly's part sits first, the first assembly's second part before its first.
    const items = [other, partTwo, loose, partOne];
    expect(groupByAssembly(items).map((it) => it.ref)).toEqual(['sre#30', 'sre#21', 'sre#22', 'sre#23']);
  });

  it('leads an assembly with its own card', () => {
    expect(groupByAssembly([partTwo, asmCard, partOne]).map((it) => it.ref)).toEqual(['sre#20', 'sre#21', 'sre#22']);
  });

  it('keeps the cards of no assembly in their own order', () => {
    expect(groupByAssembly([loose, ready]).map((it) => it.ref)).toEqual(['sre#23', 'sre#24']);
  });
});

describe('arrangeBoard with the grouping', () => {
  it('orders each station’s cards by assembly and keeps the filter', () => {
    const grouped = arrangeBoard(twoStations, { group: 'assembly', assembly: ASM });
    expect(refs(grouped)).toEqual(['sre#20', 'sre#21', 'sre#22']);
  });
});
