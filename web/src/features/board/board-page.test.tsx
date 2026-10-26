// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Board, BoardLane, BoardStation, Factory, ItemCard, OrgSummary } from '../../api/types';
import { renderBoardPage } from '../../test/route';
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

const EXPEDITE = 'Checkout API p95 above 900 ms';
const STANDARD = 'Triage the nightly export';
const BUILD = 'Ship the retry fix';

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

const expediteItem = item({
  id: 'item-12',
  ref: 'sre#12',
  number: 12,
  title: EXPEDITE,
  type: 'incident',
  class_of_service: 'expedite',
  state: 'in_progress',
  agent: 'investigator',
  slot: 2,
  turn: 2,
});
const standardItem = item({ id: 'item-7', ref: 'sre#7', number: 7, title: STANDARD });
const buildItem = item({
  id: 'item-9',
  ref: 'sre#9',
  number: 9,
  title: BUILD,
  station: 'build',
  state: 'in_progress',
  agent: 'builder',
  slot: 1,
  turn: 4,
});

/** Two agent stations, the build one the constraint; one expedite item sits in investigate. */
const board = (
  over: Partial<Board['line']> = {},
  investigateItems: ItemCard[] = [expediteItem, standardItem],
): Board => ({
  factory: { id: factory.id, name: factory.name, title: 'SRE' },
  stations: [
    station({ id: 'investigate', items: investigateItems, in_progress: 1, wip_limit: 2 }),
    station({ id: 'build', agents: ['builder'], items: [buildItem], in_progress: 1, wip_limit: 1 }),
  ],
  constraint: 'build',
  constraint_source: 'configured',
  line: {
    status: 'running',
    andon: null,
    wip: { in_progress: 2, limit: 3 },
    release: { policy: 'push', enforced: false, drum: 'build', buffer: null, in_buffer: 0 },
    expedite: { limit: 1, in_use: 1 },
    budget: null,
    touches: { finished: 0, per_item_mean: 0, per_item_median: 0, touchless_rate: 0 },
    ...over,
  },
});

describe('BoardPage', () => {
  it('shows an expedite item once, in its station, with the expedite style', async () => {
    renderBoardPage({ page: BoardPage, org, factory, board: board() });

    const columns = await screen.findByRole('region', { name: 'Stations' });
    // The phone's picker shows only the picked station, so the whole page holds the item once.
    expect(screen.getAllByRole('link', { name: EXPEDITE })).toHaveLength(1);

    const investigate = within(columns).getByRole('region', { name: 'Investigate' });
    const card = within(investigate).getByRole('link', { name: EXPEDITE }).closest('article');
    expect(card).not.toBeNull();
    // Expedite first within the station, as the server orders it.
    expect(within(investigate).getAllByRole('article')[0]).toBe(card);
    expect(card?.classList).toContain('bg-rework-tint');
    expect(card?.classList).toContain('border-rework-line');
    expect(within(card as HTMLElement).getByText('Expedite')).toBeTruthy();

    // A standard card in the same station has neither the tint nor the label.
    const standard = within(investigate).getByRole('link', { name: STANDARD }).closest('article');
    expect(standard?.classList).not.toContain('bg-rework-tint');
    expect(within(standard as HTMLElement).queryByText('Expedite')).toBeNull();
  });

  it('the selected station tab shows only its own items', async () => {
    const first = renderBoardPage({ page: BoardPage, org, factory, board: board(), station: 'investigate' });
    const investigatePanel = await screen.findByRole('tabpanel');
    expect(within(investigatePanel).getByRole('link', { name: EXPEDITE })).toBeTruthy();
    expect(within(investigatePanel).getByRole('link', { name: STANDARD })).toBeTruthy();
    expect(within(investigatePanel).queryByRole('link', { name: BUILD })).toBeNull();
    first.unmount();

    renderBoardPage({ page: BoardPage, org, factory, board: board(), station: 'build' });
    const buildPanel = await screen.findByRole('tabpanel');
    expect(within(buildPanel).getByRole('link', { name: BUILD })).toBeTruthy();
    expect(within(buildPanel).queryByRole('link', { name: EXPEDITE })).toBeNull();
    expect(within(buildPanel).queryByRole('link', { name: STANDARD })).toBeNull();
  });

  it('heads the line with the expedite count in use', async () => {
    renderBoardPage({ page: BoardPage, org, factory, board: board() });

    const line = await screen.findByRole('region', { name: 'The line' });
    expect(within(line).getByText('WIP 2 of 3')).toBeTruthy();
    expect(within(line).getByText('Expedite 1 of 1 in use')).toBeTruthy();
    // The phone's status line is the line, WIP and touches only, so the count appears once.
    expect(screen.getAllByText('Expedite 1 of 1 in use')).toHaveLength(1);
  });

  it('leaves the expedite count out when the line has no lane and no expedite item', async () => {
    const plain = board({ expedite: null }, [standardItem]);
    renderBoardPage({ page: BoardPage, org, factory, board: plain });

    const line = await screen.findByRole('region', { name: 'The line' });
    expect(within(line).getByText('WIP 2 of 3')).toBeTruthy();
    expect(screen.queryByText(/^Expedite/)).toBeNull();
  });

  it('the header Inbox button opens the inbox drawer over the board', async () => {
    renderBoardPage({ page: BoardPage, org, factory, board: board() });

    fireEvent.click(await screen.findByRole('button', { name: /^Inbox/ }));
    const drawer = await screen.findByRole('dialog', { name: 'Inbox' });
    expect(await within(drawer).findByText('Nothing needs you')).toBeTruthy();
  });

  it('lays the phone picker out as one-line tabs, the constraint’s with its amber edge and the picked one ink-filled', async () => {
    renderBoardPage({
      page: () => (
        <div style={{ width: 390 }}>
          <BoardPage />
        </div>
      ),
      org,
      factory,
      board: board(),
      station: 'build',
    });

    const tabs = await screen.findByRole('tablist', { name: 'Stations' });
    const build = within(tabs).getByRole('tab', { name: /^Build/ });
    expect(build.getAttribute('data-state')).toBe('active');
    expect(build.classList).toContain('data-[state=active]:bg-ink');
    expect(build.classList).toContain('shadow-[inset_0_-3px_0_var(--color-amber)]');
    expect(build.classList).toContain('min-h-14');
    // The name and the load are each one line, the load cut with its full text in a title.
    const load = within(build).getByText('1 of 1');
    expect(load.classList).toContain('truncate');
    expect(load.getAttribute('title')).toBe('1 of 1');

    const investigate = within(tabs).getByRole('tab', { name: /^Investigate/ });
    expect(investigate.getAttribute('data-state')).toBe('inactive');
    expect(investigate.classList).not.toContain('shadow-[inset_0_-3px_0_var(--color-amber)]');
  });

  it('states the constraint under the tabs only while the constraint is picked', async () => {
    const first = renderBoardPage({ page: BoardPage, org, factory, board: board(), station: 'build' });
    expect(await screen.findByText('Build is the constraint: 1 of 1 busy, 0 waiting')).toBeTruthy();
    first.unmount();

    renderBoardPage({ page: BoardPage, org, factory, board: board(), station: 'investigate' });
    await screen.findByRole('tabpanel');
    expect(screen.queryByText(/^Build is the constraint: /)).toBeNull();
  });

  it('keeps the phone status line to one line with the touches per item and a 44 px link to them', async () => {
    const touched = board({ touches: { finished: 4, per_item_mean: 1.4, per_item_median: 1, touchless_rate: 0.5 } });
    renderBoardPage({ page: BoardPage, org, factory, board: touched });

    const line = await screen.findByTitle('Line running, WIP 2 of 3, 1.4 touches per item');
    expect(line.classList).toContain('truncate');
    expect(line.textContent).toContain('1.4 touches per item');

    const link = screen.getByRole('link', { name: 'By station' });
    expect(link.classList).toContain('min-h-11');
    expect(line.contains(link)).toBe(false);
  });

  it('shows no touches link while no item finished this week', async () => {
    renderBoardPage({ page: BoardPage, org, factory, board: board() });

    await screen.findByTitle('Line running, WIP 2 of 3');
    expect(screen.queryByRole('link', { name: 'By station' })).toBeNull();
  });
});

describe('board filters and grouping', () => {
  const ASM1 = 'item-asm1';
  const ASM1_TITLE = 'Checkout retry rollout';
  const ADD = 'Add the retry budget';
  const DRAFT = 'Draft the runbook';
  const WIRE = 'Wire the retry flag';
  const TRIAGE = 'Triage the nightly export';
  const ROTATE = 'Rotate the staging keys';
  const SHIP = 'Ship the retry fix';

  const lane = (over: Partial<BoardLane>): BoardLane => ({
    key: 'investigator',
    kind: 'agent',
    role: 'Investigator',
    model: 'claude-opus-5-5',
    model_label: 'Opus 5.5',
    slots: 2,
    busy: 1,
    ...over,
  });
  const part = (id: string, title: string, n: number, parts: number) => ({
    id,
    ref: `sre#${id}`,
    title,
    part: n,
    parts,
  });

  /**
   * Investigate: the docs assembly's first part, a loose waiting card, the checkout assembly's second
   * part, its first part working, and a loose card for the triager. Build: a working card of its own.
   */
  const filterBoard = (): Board => {
    const investigate = station({
      id: 'investigate',
      lanes: [lane({}), lane({ key: 'triager', role: 'Triager', model_label: 'Haiku 5.5', slots: 1, busy: 0 })],
      items: [
        item({
          id: 'item-30',
          ref: 'sre#30',
          title: DRAFT,
          board_lane: 'investigator',
          assembly: part('item-asm2', 'Docs refresh', 1, 1),
        }),
        item({ id: 'item-23', ref: 'sre#23', title: TRIAGE, board_lane: 'investigator' }),
        item({
          id: 'item-22',
          ref: 'sre#22',
          title: WIRE,
          board_lane: 'investigator',
          assembly: part(ASM1, ASM1_TITLE, 2, 2),
        }),
        item({
          id: 'item-21',
          ref: 'sre#21',
          title: ADD,
          state: 'in_progress',
          agent: 'investigator',
          slot: 1,
          turn: 2,
          board_lane: 'investigator',
          assembly: part(ASM1, ASM1_TITLE, 1, 2),
        }),
        item({ id: 'item-26', ref: 'sre#26', title: ROTATE, board_lane: 'triager' }),
      ],
      in_progress: 1,
      wip_limit: 2,
    });
    const build = station({
      id: 'build',
      agents: ['builder'],
      lanes: [lane({ key: 'builder', role: 'Builder', model_label: 'Sonnet 5.5', slots: 1, busy: 1 })],
      items: [
        item({
          id: 'item-24',
          ref: 'sre#24',
          title: SHIP,
          station: 'build',
          state: 'in_progress',
          agent: 'builder',
          slot: 1,
          turn: 4,
          board_lane: 'builder',
        }),
      ],
      in_progress: 1,
      wip_limit: 1,
    });
    return {
      factory: { id: factory.id, name: factory.name, title: 'SRE' },
      stations: [investigate, build],
      constraint: 'build',
      constraint_source: 'configured',
      line: {
        status: 'running',
        andon: null,
        wip: { in_progress: 2, limit: 3 },
        release: { policy: 'push', enforced: false, drum: 'build', buffer: null, in_buffer: 0 },
        expedite: { limit: 1, in_use: 0 },
        budget: null,
        touches: { finished: 0, per_item_mean: 0, per_item_median: 0, touchless_rate: 0 },
      },
    };
  };

  /** The titles of the cards shown in the board's columns, in the order they render. */
  const shownTitles = (region: HTMLElement = screen.getByRole('region', { name: 'Stations' })) =>
    within(region)
      .queryAllByRole('link')
      .map((link) => link.textContent);

  /** A filter's chosen value: the option's value, '' for Any. */
  const choice = (name: string) => (screen.getByRole('combobox', { name }) as HTMLSelectElement).value;

  it('filters the cards by assembly, agent role and status, in every station', async () => {
    renderBoardPage({ page: BoardPage, org, factory, board: filterBoard() });
    await screen.findByRole('region', { name: 'Stations' });
    const filter = async (name: string, value: string, titles: string[]) => {
      fireEvent.change(screen.getByRole('combobox', { name }), { target: { value } });
      await waitFor(() => expect(shownTitles()).toEqual(titles));
    };
    await filter('Assembly', ASM1, [ADD, WIRE]);
    expect(screen.queryByRole('link', { name: DRAFT })).toBeNull();

    await filter('Assembly', '', [ADD, DRAFT, TRIAGE, WIRE, ROTATE, SHIP]);
    await filter('Agent role', 'Builder', [SHIP]);
    await filter('Agent role', '', [ADD, DRAFT, TRIAGE, WIRE, ROTATE, SHIP]);
    await filter('Status', 'working', [ADD, SHIP]);
    await filter('Status', 'waiting', [DRAFT, TRIAGE, WIRE, ROTATE]);
    await filter('Status', 'same-files', []);
  });

  it('keeps the lane and its header, and says nothing to do where the filter leaves no card', async () => {
    renderBoardPage({ page: BoardPage, org, factory, board: filterBoard() });
    await screen.findByRole('region', { name: 'Stations' });

    fireEvent.change(screen.getByRole('combobox', { name: 'Agent role' }), { target: { value: 'Triager' } });
    await waitFor(() =>
      expect(within(screen.getByRole('region', { name: 'Investigator' })).queryAllByRole('link')).toHaveLength(0),
    );
    const investigator = screen.getByRole('region', { name: 'Investigator' });
    expect(within(investigator).getByRole('heading', { name: 'Investigator' })).toBeTruthy();
    expect(within(investigator).getByText('Nothing to do')).toBeTruthy();
    expect(shownTitles(screen.getByRole('region', { name: 'Triager' }))).toEqual([ROTATE]);
  });

  it('orders a lane’s cards by assembly when grouped, and keeps the order of the board otherwise', async () => {
    const grouped = renderBoardPage({
      page: BoardPage,
      org,
      factory,
      board: filterBoard(),
      search: { group: 'assembly' },
    });
    const lane = await screen.findByRole('region', { name: 'Investigator' });
    expect(shownTitles(lane)).toEqual([ADD, DRAFT, WIRE, TRIAGE]);
    grouped.unmount();

    renderBoardPage({ page: BoardPage, org, factory, board: filterBoard() });
    const ungrouped = await screen.findByRole('region', { name: 'Investigator' });
    expect(shownTitles(ungrouped)).toEqual([ADD, DRAFT, TRIAGE, WIRE]);
  });

  it('shows the view its search params name, and writes each change back to the URL', async () => {
    const { router } = renderBoardPage({
      page: BoardPage,
      org,
      factory,
      board: filterBoard(),
      search: { assembly: ASM1, group: 'assembly' },
    });
    await screen.findByRole('region', { name: 'Stations' });
    const search = () => router.state.location.search as Record<string, unknown>;
    expect(choice('Assembly')).toBe(ASM1);
    expect(shownTitles()).toEqual([ADD, WIRE]);

    fireEvent.change(screen.getByRole('combobox', { name: 'Status' }), { target: { value: 'waiting' } });
    await waitFor(() => expect(search().status).toBe('waiting'));
    expect(search().assembly).toBe(ASM1);

    fireEvent.change(screen.getByRole('combobox', { name: 'Assembly' }), { target: { value: '' } });
    await waitFor(() => expect(search().assembly).toBeUndefined());
    expect(search().group).toBe('assembly');

    fireEvent.click(screen.getByRole('radio', { name: 'None' }));
    await waitFor(() => expect(search().group).toBeUndefined());
  });

  it('drops the search params the board cannot show, so the page shows every card', async () => {
    renderBoardPage({
      page: BoardPage,
      org,
      factory,
      board: filterBoard(),
      search: { assembly: 'item-gone', role: 'Infra engineer', status: 'blocked', group: 'lane' },
    });
    await screen.findByRole('region', { name: 'Stations' });
    expect(choice('Assembly')).toBe('');
    expect(choice('Agent role')).toBe('');
    expect(choice('Status')).toBe('');
    expect(screen.getByRole('radio', { name: 'None' })).toHaveProperty('checked', true);
    expect(shownTitles()).toEqual([ADD, DRAFT, TRIAGE, WIRE, ROTATE, SHIP]);
  });
});
