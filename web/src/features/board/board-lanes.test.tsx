// @vitest-environment jsdom
import { cleanup, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Board, BoardLane, BoardStation, Factory, ItemCard, OrgSummary } from '../../api/types';
import { renderBoardPage } from '../../test/route';
import { StationBody } from './board-lane';
import { BoardPage } from './board-page';

// The header's Inbox button reads the inbox; the board tests see an empty one.
vi.mock(import('../../api/client'), async (original) => ({
  ...(await original()),
  getInbox: vi.fn(async () => ({ entries: [], truncated: false })),
}));

afterEach(cleanup);

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
};

const item = (over: Partial<ItemCard>): ItemCard => ({
  id: 'item-1',
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
  model: 'claude-sonnet-5-5',
  model_label: 'Sonnet 5.5',
  slots: 2,
  busy: 1,
  ...over,
});

const station = (over: Partial<BoardStation>): BoardStation => ({
  id: 'investigate',
  kind: 'agent',
  agents: ['investigator'],
  wip_limit: 2,
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

const WORKING = 'Checkout API p95 above 900 ms';
const QUEUED = 'Triage the nightly export';
const STRAY = 'Rotate the staging keys';

/** One agent station with two lanes: the investigator works one item and has one queued; the triager has nothing. */
const board = (over: Partial<BoardStation> = {}, items: ItemCard[] = []): Board => ({
  factory: { id: factory.id, name: factory.name, title: 'SRE' },
  stations: [
    station({
      lanes: [lane({}), lane({ key: 'triager', role: 'Triager', model: null, model_label: null, slots: 2, busy: 0 })],
      items,
      in_progress: 1,
      ...over,
    }),
  ],
  constraint: 'investigate',
  constraint_source: 'configured',
  line: {
    status: 'running',
    andon: null,
    wip: { in_progress: 1, limit: 2 },
    release: { policy: 'push', enforced: false, drum: null, buffer: null, in_buffer: 0 },
    expedite: null,
    budget: null,
    touches: { finished: 0, per_item_mean: 0, per_item_median: 0, touchless_rate: 0 },
  },
});

const workItem = item({
  id: 'item-12',
  ref: 'sre#12',
  number: 12,
  title: WORKING,
  state: 'in_progress',
  agent: 'investigator',
  slot: 2,
  turn: 3,
  board_lane: 'investigator',
  touch_count: 2,
});
const queuedItem = item({ id: 'item-7', ref: 'sre#7', number: 7, title: QUEUED, board_lane: 'investigator' });
const strayItem = item({
  id: 'item-9',
  ref: 'sre#9',
  number: 9,
  title: STRAY,
  state: 'in_progress',
  board_lane: 'gone',
});

describe('board lanes', () => {
  it('renders one lane per station lane in API order, headed by role, model and slot boxes', async () => {
    renderBoardPage({ page: BoardPage, org, factory, board: board({}, [workItem, queuedItem]) });

    const columns = await screen.findByRole('region', { name: 'Stations' });
    const investigate = within(columns).getByRole('region', { name: 'Investigate' });
    const lanes = within(investigate).getAllByRole('region');
    expect(lanes.map((l) => l.getAttribute('aria-label'))).toEqual(['Investigator', 'Triager']);

    const investigator = lanes[0] as HTMLElement;
    expect(within(investigator).getByText('Sonnet 5.5')).toBeTruthy();
    // Two slots, one of them busy: one filled box, one outlined.
    expect(within(investigator).getByText('1 of 2 slots busy')).toBeTruthy();
    const boxes = investigator.querySelectorAll('[aria-hidden="true"].size-2\\.5');
    expect([...boxes].map((b) => b.classList.contains('bg-run'))).toEqual([true, false]);

    // A lane with no model and no card of its own says so.
    const triager = lanes[1] as HTMLElement;
    expect(within(triager).queryByText('Sonnet 5.5')).toBeNull();
    expect(within(triager).getByText('Nothing to do')).toBeTruthy();
  });

  it('shows a person lane without slot boxes at a human station', async () => {
    const approve: Board = {
      ...board(),
      stations: [
        station({
          id: 'approve',
          kind: 'human',
          agents: [],
          wip_limit: null,
          lanes: [
            { key: 'person', kind: 'person', role: 'Approve', model: null, model_label: null, slots: null, busy: 0 },
          ],
          items: [],
        }),
      ],
    };
    renderBoardPage({ page: BoardPage, org, factory, board: approve });

    const columns = await screen.findByRole('region', { name: 'Stations' });
    // The column and its lane are both named Approve; the column comes first, the lane inside it.
    const [column] = within(columns).getAllByRole('region', { name: 'Approve' });
    const [person] = within(column as HTMLElement).getAllByRole('region');
    expect(within(person as HTMLElement).getByRole('heading', { name: 'Approve' })).toBeTruthy();
    expect(within(person as HTMLElement).queryByText(/slots busy/)).toBeNull();
  });

  it('puts a card in the lane its board_lane names, and a card no lane claims in an untitled lane at the foot', async () => {
    renderBoardPage({ page: BoardPage, org, factory, board: board({}, [workItem, strayItem, queuedItem]) });

    const investigate = within(await screen.findByRole('region', { name: 'Stations' })).getByRole('region', {
      name: 'Investigate',
    });
    const investigator = within(investigate).getByRole('region', { name: 'Investigator' });
    expect(within(investigator).getByRole('link', { name: WORKING })).toBeTruthy();
    expect(within(investigator).getByRole('link', { name: QUEUED })).toBeTruthy();
    expect(within(investigator).queryByRole('link', { name: STRAY })).toBeNull();

    const triager = within(investigate).getByRole('region', { name: 'Triager' });
    expect(within(triager).queryByRole('link', { name: STRAY })).toBeNull();
    // The unclaimed card is still in the station, below the lanes.
    expect(within(investigate).getByRole('link', { name: STRAY })).toBeTruthy();
  });

  it('lists a lane’s work cards (doing line, touches) above its waiting rows (reason)', async () => {
    renderBoardPage({ page: BoardPage, org, factory, board: board({}, [queuedItem, workItem]) });

    const investigator = within(
      within(await screen.findByRole('region', { name: 'Stations' })).getByRole('region', { name: 'Investigate' }),
    ).getByRole('region', { name: 'Investigator' });
    const [card, row] = within(investigator).getAllByRole('article');
    expect(within(card as HTMLElement).getByRole('link', { name: WORKING })).toBeTruthy();
    expect(within(card as HTMLElement).getByText('Working, turn 3')).toBeTruthy();
    expect(within(card as HTMLElement).getByText('2 touches')).toBeTruthy();

    expect(within(row as HTMLElement).getByRole('link', { name: QUEUED })).toBeTruthy();
    expect(within(row as HTMLElement).getByText('Waiting')).toBeTruthy();
    expect(within(row as HTMLElement).queryByText('2 touches')).toBeNull();
  });

  it('shows no touches on a card nobody touched', async () => {
    renderBoardPage({ page: BoardPage, org, factory, board: board({}, [{ ...workItem, touch_count: 0 }]) });

    const columns = await screen.findByRole('region', { name: 'Stations' });
    const card = within(columns).getByRole('link', { name: WORKING }).closest('article') as HTMLElement;
    expect(within(card).queryByText(/touch/)).toBeNull();
  });

  it('never shows a numbered agent copy on a card, row or lane', async () => {
    renderBoardPage({ page: BoardPage, org, factory, board: board({}, [workItem, queuedItem]) });

    await screen.findByRole('region', { name: 'Stations' });
    expect(screen.queryByText(/investigator-\d+\b/)).toBeNull();
    expect(screen.queryByText(/triager-\d+\b/)).toBeNull();
  });

  it('truncates a long role, model, doing line and reason on one line with the full text in a title', async () => {
    const longRole = `Senior ${'infrastructure '.repeat(20)}engineer`;
    const longTitle = `Incident ${'postmortem '.repeat(25)}follow-up`;
    const working = item({
      id: 'item-30',
      ref: 'sre#30',
      number: 30,
      title: longTitle,
      state: 'in_progress',
      turn: 5,
      board_lane: 'long',
    });
    const longBoard = board({ lanes: [lane({ key: 'long', role: longRole, model_label: longRole })] }, [working]);
    renderBoardPage({ page: BoardPage, org, factory, board: longBoard });

    const columns = await screen.findByRole('region', { name: 'Stations' });
    const card = within(columns).getByRole('link', { name: longTitle }).closest('article') as HTMLElement;
    // The title clamps to two lines and carries its full text.
    const link = within(card).getByRole('link', { name: longTitle });
    expect(link.classList).toContain('line-clamp-2');
    expect(link.getAttribute('title')).toBe(longTitle);

    const roleHeading = within(columns).getByRole('heading', { name: longRole });
    expect(roleHeading.classList).toContain('truncate');
    expect(roleHeading.getAttribute('title')).toBe(longRole);

    const doing = within(card).getByText('Working, turn 5');
    expect(doing.classList).toContain('truncate');
    expect(doing.parentElement?.getAttribute('title')).toBe('Working, turn 5');
  });

  it('shows a waiting row’s reason with the clock for a queued item and no icon for a blocked one', async () => {
    const blocked = item({
      id: 'item-11',
      ref: 'sre#11',
      number: 11,
      title: 'Retry the export',
      state: 'blocked',
      board_lane: 'investigator',
    });
    renderBoardPage({ page: BoardPage, org, factory, board: board({}, [queuedItem, blocked]) });

    const columns = await screen.findByRole('region', { name: 'Stations' });
    const queuedRow = within(columns).getByRole('link', { name: QUEUED }).closest('article') as HTMLElement;
    expect(queuedRow.querySelector('.lucide-clock')).not.toBeNull();

    const blockedRow = within(columns)
      .getByRole('link', { name: 'Retry the export' })
      .closest('article') as HTMLElement;
    expect(within(blockedRow).getByText('Blocked, needs a retry')).toBeTruthy();
    expect(blockedRow.querySelector('.lucide-clock')).toBeNull();
  });

  it('keeps the expedite tint and label on a queued expedite row, and gives a standard row neither', async () => {
    const expediteRow = item({
      id: 'item-21',
      ref: 'sre#21',
      number: 21,
      title: 'Restore the export job',
      class_of_service: 'expedite',
      board_lane: 'investigator',
    });
    renderBoardPage({ page: BoardPage, org, factory, board: board({}, [expediteRow, queuedItem]) });

    const investigator = within(
      within(await screen.findByRole('region', { name: 'Stations' })).getByRole('region', { name: 'Investigate' }),
    ).getByRole('region', { name: 'Investigator' });
    const expedited = within(investigator).getByRole('link', { name: 'Restore the export job' }).closest('article');
    expect(expedited?.classList.contains('bg-rework-tint')).toBe(true);
    expect(within(expedited as HTMLElement).getByText('Expedite')).toBeTruthy();

    const standard = within(investigator).getByRole('link', { name: QUEUED }).closest('article');
    expect(standard?.classList.contains('bg-rework-tint')).toBe(false);
    expect(within(standard as HTMLElement).queryByText('Expedite')).toBeNull();
  });

  it('gives a compact station’s lane cards their 44 px Move buttons, and the desktop’s 32 px ones', async () => {
    const stationOf = board({}, [workItem, queuedItem]).stations[0] as BoardStation;
    const render = (compact: boolean) =>
      renderBoardPage({
        page: () => <StationBody station={stationOf} onMove={vi.fn()} draggable={false} compact={compact} />,
        org,
        factory,
      });

    const phone = render(true);
    const compactButtons = await screen.findAllByRole('button', { name: /^Move sre#/ });
    expect(compactButtons).toHaveLength(2);
    for (const button of compactButtons) expect(button.classList).toContain('size-11');
    phone.unmount();

    render(false);
    const desktopButtons = await screen.findAllByRole('button', { name: /^Move sre#/ });
    expect(desktopButtons).toHaveLength(2);
    for (const button of desktopButtons) expect(button.classList).toContain('size-8');
  });

  it('says Nothing to do in a phone lane with no items', async () => {
    renderBoardPage({ page: BoardPage, org, factory, board: board({}, [workItem, queuedItem]) });

    const panel = await screen.findByRole('tabpanel');
    const triager = within(panel).getByRole('region', { name: 'Triager' });
    expect(within(triager).getByText('Nothing to do')).toBeTruthy();
  });

  it('shows a work card’s and a waiting row’s assembly line, linking to the assembly’s item page', async () => {
    const assembly = { id: 'item-40', ref: 'sre#40', title: 'Migrate billing', part: 4, parts: 6 };
    renderBoardPage({
      page: BoardPage,
      org,
      factory,
      board: board({}, [
        { ...workItem, assembly },
        { ...queuedItem, assembly },
      ]),
    });

    const columns = await screen.findByRole('region', { name: 'Stations' });
    const work = within(columns).getByRole('link', { name: WORKING }).closest('article') as HTMLElement;
    const row = within(columns).getByRole('link', { name: QUEUED }).closest('article') as HTMLElement;
    for (const card of [work, row]) {
      const line = within(card).getByRole('link', { name: 'Part 4 of 6 of Migrate billing' });
      expect(line.getAttribute('href')).toBe('/o/acme/f/sre/items/40');
      expect(within(line).getByText('Migrate billing').getAttribute('title')).toBe('Migrate billing');
      expect(within(line).getByText('› 4 of 6').classList).toContain('whitespace-nowrap');
      expect(within(line).getByText('Migrate billing').classList).toContain('truncate');
    }
  });

  it('keeps the assembly title to one line with an ellipsis, and the part count whole', async () => {
    const longTitle = `Migrate ${'the billing ledger '.repeat(10)}end`;
    const assembly = { id: 'item-41', ref: 'sre#41', title: longTitle, part: 12, parts: 14 };
    renderBoardPage({ page: BoardPage, org, factory, board: board({}, [{ ...workItem, assembly }]) });

    const columns = await screen.findByRole('region', { name: 'Stations' });
    const line = within(columns).getByRole('link', { name: `Part 12 of 14 of ${longTitle}` });
    expect(within(line).getByText(longTitle).classList).toContain('truncate');
    expect(within(line).getByText('› 12 of 14').classList).toContain('shrink-0');
    expect(line.classList).toContain('relative');
    expect(line.classList).toContain('z-10');
  });

  it('shows no assembly line on a card or a row whose item is no part', async () => {
    renderBoardPage({
      page: BoardPage,
      org,
      factory,
      board: board({}, [{ ...workItem, assembly: null }, { ...queuedItem }]),
    });

    const columns = await screen.findByRole('region', { name: 'Stations' });
    expect(within(columns).queryByRole('link', { name: /^Part \d+ of/ })).toBeNull();
    expect(within(columns).getByRole('link', { name: WORKING })).toBeTruthy();
    expect(within(columns).getByRole('link', { name: QUEUED })).toBeTruthy();
  });

  it('shows the assembly line on the phone’s compact lanes too', async () => {
    const assembly = { id: 'item-40', ref: 'sre#40', title: 'Migrate billing', part: 4, parts: 6 };
    const stationOf = board({}, [
      { ...workItem, assembly },
      { ...queuedItem, assembly },
    ]).stations[0] as BoardStation;
    renderBoardPage({
      page: () => <StationBody station={stationOf} onMove={vi.fn()} draggable={false} compact />,
      org,
      factory,
    });

    const lines = await screen.findAllByRole('link', { name: 'Part 4 of 6 of Migrate billing' });
    expect(lines).toHaveLength(2);
    for (const line of lines) expect(line.getAttribute('href')).toBe('/o/acme/f/sre/items/40');
  });
});
