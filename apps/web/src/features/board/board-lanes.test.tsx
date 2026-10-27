// @vitest-environment jsdom
import { cleanup, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Board, BoardLane, BoardStation, Factory, ItemCard, ItemWaiting, OrgSummary } from '../../api/types';
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
  assemblies: [],
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

/** The phone's picked station: the one tab panel (jsdom draws the desktop columns too, so the names repeat). */
async function phonePanel(): Promise<HTMLElement> {
  await waitFor(() => expect(document.querySelector('[role="tabpanel"]')).not.toBeNull());
  return document.querySelector('[role="tabpanel"]') as HTMLElement;
}

describe('board lanes', () => {
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
    renderBoardPage({ page: BoardPage, org, factory, board: approve, station: 'approve' });

    const columns = await phonePanel();
    // The station's panel is named Approve, and its lane inside it.
    const [person] = within(columns).getAllByRole('region');
    expect(within(person as HTMLElement).getAllByRole('heading', { name: 'Approve' }).length).toBeGreaterThan(0);
    expect(within(person as HTMLElement).queryByText(/slots busy/)).toBeNull();
  });

  it('shows no touches on a card nobody touched', async () => {
    renderBoardPage({ page: BoardPage, org, factory, board: board({}, [{ ...workItem, touch_count: 0 }]) });

    const columns = await phonePanel();
    const card = within(columns).getByRole('link', { name: WORKING }).closest('article') as HTMLElement;
    expect(within(card).queryByText(/touch/)).toBeNull();
  });

  it('shows no plan bar on a card whose run has no plan steps', async () => {
    renderBoardPage({ page: BoardPage, org, factory, board: board({}, [{ ...workItem, plan_steps: null }]) });

    await phonePanel();
    expect(screen.queryByRole('img', { name: /steps/ })).toBeNull();
  });

  it('shows the doing line and the plan bar on the phone’s compact lane cards too', async () => {
    const planned = item({
      ...workItem,
      activity: 'Searching for retry handlers',
      plan_steps: { done: 1, total: 3 },
    });
    const stationOf = board({}, [planned]).stations[0] as BoardStation;
    renderBoardPage({
      page: () => <StationBody station={stationOf} onMove={vi.fn()} draggable={false} compact />,
      org,
      factory,
    });

    expect(await screen.findByText('Searching for retry handlers')).toBeTruthy();
    expect(screen.getByRole('img', { name: '1 of 3 steps' })).toBeTruthy();
    expect(screen.getByRole('button', { name: /^Move sre#12/ }).classList).toContain('size-11');
  });

  it('never shows a numbered agent copy on a card, row or lane', async () => {
    renderBoardPage({ page: BoardPage, org, factory, board: board({}, [workItem, queuedItem]) });

    await phonePanel();
    expect(screen.queryByText(/investigator-\d+\b/)).toBeNull();
    expect(screen.queryByText(/triager-\d+\b/)).toBeNull();
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

    const columns = await phonePanel();
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

    const columns = await phonePanel();
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

    const columns = await phonePanel();
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

describe('board waiting rows', () => {
  const waitingRow = (waiting: ItemWaiting | null) =>
    board({}, [item({ id: 'item-7', ref: 'sre#7', number: 7, title: QUEUED, board_lane: 'investigator', waiting })]);

  it('shows one wait line for one needed item, its full list as the tooltip, and no list or link to it', async () => {
    renderBoardPage({
      page: BoardPage,
      org,
      factory,
      board: waitingRow({
        kind: 'files',
        items: ['sre#3'],
        files: ['terraform/main.tf'],
        needs: [{ ref: 'sre#3', title: 'Move the state bucket', paths: ['terraform/**', 'terraform/main.tf'] }],
      }),
    });

    const [link] = await screen.findAllByRole('link', { name: QUEUED });
    const row = link?.closest('article') as HTMLElement;
    const line = within(row).getByText('Waits for sre#3');
    expect(line.parentElement?.getAttribute('title')).toBe(
      'sre#3 Move the state bucket (same files terraform/**, terraform/main.tf)\nShared files: terraform/main.tf',
    );
    expect(within(row).queryByRole('link', { name: 'sre#3' })).toBeNull();
    expect(row.textContent).not.toMatch(/\*\*|terraform\//);
  });

  it('counts the other needed items in the one line: "+2" for three', async () => {
    renderBoardPage({
      page: BoardPage,
      org,
      factory,
      board: waitingRow({
        kind: 'after',
        items: ['sre#2', 'sre#4', 'sre#5'],
        files: [],
        needs: [
          { ref: 'sre#2', title: 'Set up the drum', paths: [] },
          { ref: 'sre#4', title: 'Split the export', paths: [] },
          { ref: 'sre#5', title: 'Move the bucket', paths: [] },
        ],
      }),
    });

    const [link] = await screen.findAllByRole('link', { name: QUEUED });
    const row = link?.closest('article') as HTMLElement;
    expect(within(row).getByText('Waits for sre#2 +2')).toBeTruthy();
    expect(row.querySelectorAll('.truncate')).toHaveLength(1);
  });

  it('adds no wait line to a row that does not wait on an item', async () => {
    renderBoardPage({ page: BoardPage, org, factory, board: waitingRow(null) });

    const [link] = await screen.findAllByRole('link', { name: QUEUED });
    const row = link?.closest('article') as HTMLElement;
    expect(within(row).queryByText(/^Waits for/)).toBeNull();
    expect(row.querySelector('[title^="sre#"]')).toBeNull();
  });
});
