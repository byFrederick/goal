// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Board, BoardLane, BoardStation, Factory, ItemCard, OrgSummary } from '../../api/types';
import { ShellFrame } from '../../app/app-shell';
import { renderBoardPage } from '../../test/route';
import { TooltipProvider } from '../../ui/tooltip';
import { BoardPage } from './board-page';

/** The Board as the app draws it: inside ShellFrame, so the desktop top bar holds its search and actions. */
const ShellBoard = () => (
  <TooltipProvider>
    <ShellFrame>
      <BoardPage />
    </ShellFrame>
  </TooltipProvider>
);

// The header's Needs you link reads the inbox for its count; the board tests see an empty one.
vi.mock(import('../../api/client'), async (original) => ({
  ...(await original()),
  getInbox: vi.fn(async () => ({ entries: [], truncated: false })),
}));

afterEach(() => {
  cleanup();
  // @ts-expect-error restoring the jsdom default, which has no matchMedia
  delete window.matchMedia;
});

/** Pretends the viewport is a phone (below 768 px): the phone's status line and filters draw then. */
function stubPhone() {
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: true,
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  }));
}

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
  assemblies: [],
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
    renderBoardPage({ page: ShellBoard, org, factory, board: board() });

    const columns = await screen.findByRole('region', { name: 'Stations' });
    // The phone's picker shows only the picked station, so the whole page holds the item once.
    expect(screen.getAllByRole('link', { name: EXPEDITE })).toHaveLength(1);

    const investigate = within(columns).getByRole('region', { name: 'Investigate' });
    const card = within(investigate).getByRole('link', { name: EXPEDITE }).closest('article');
    expect(card).not.toBeNull();
    // Expedite first within the station, as the server orders it.
    expect(within(investigate).getAllByRole('article')[0]).toBe(card);
    // The expedite zone is tinted; the card keeps its own border and says Expedite in text.
    expect(card?.parentElement?.classList).toContain('bg-amber-tint');
    expect(card?.classList).not.toContain('bg-rework-tint');
    expect(within(card as HTMLElement).getByText('Expedite')).toBeTruthy();

    // A standard card in the same station has neither the tint nor the label.
    const standard = within(investigate).getByRole('link', { name: STANDARD }).closest('article');
    expect(standard?.parentElement?.classList).not.toContain('bg-amber-tint');
    expect(within(standard as HTMLElement).queryByText('Expedite')).toBeNull();
  });

  it('the selected station tab shows only its own items', async () => {
    const first = renderBoardPage({ page: ShellBoard, org, factory, board: board(), station: 'investigate' });
    const investigatePanel = await screen.findByRole('tabpanel');
    expect(within(investigatePanel).getByRole('link', { name: EXPEDITE })).toBeTruthy();
    expect(within(investigatePanel).getByRole('link', { name: STANDARD })).toBeTruthy();
    expect(within(investigatePanel).queryByRole('link', { name: BUILD })).toBeNull();
    first.unmount();

    renderBoardPage({ page: ShellBoard, org, factory, board: board(), station: 'build' });
    const buildPanel = await screen.findByRole('tabpanel');
    expect(within(buildPanel).getByRole('link', { name: BUILD })).toBeTruthy();
    expect(within(buildPanel).queryByRole('link', { name: EXPEDITE })).toBeNull();
    expect(within(buildPanel).queryByRole('link', { name: STANDARD })).toBeNull();
  });

  it('heads the columns with the expedite lane and its count in use', async () => {
    renderBoardPage({ page: ShellBoard, org, factory, board: board() });

    const columns = await screen.findByRole('region', { name: 'Stations' });
    // The chip, and the card's own Expedite label.
    expect(within(columns).getAllByText('Expedite')).toHaveLength(2);
    expect(within(columns).getByText('1 of 1 in use, jumps every queue')).toBeTruthy();
  });

  it('leaves the expedite lane out when the line has no lane and no expedite item', async () => {
    const plain = board({ expedite: null }, [standardItem]);
    renderBoardPage({ page: ShellBoard, org, factory, board: plain });

    await screen.findByRole('region', { name: 'Stations' });
    expect(screen.queryByText(/jumps every queue/)).toBeNull();
    expect(screen.queryByText('Expedite')).toBeNull();
  });

  it('a queue column has no header, and the agent, human and done columns keep theirs', async () => {
    const intake = station({ id: 'intake', kind: 'queue', agents: [], wip_limit: null, items: [standardItem] });
    const withQueue = { ...board(), stations: [intake, ...board().stations] };
    renderBoardPage({ page: ShellBoard, org, factory, board: withQueue });

    const columns = await screen.findByRole('region', { name: 'Stations' });
    const queue = within(columns).getByRole('region', { name: 'Intake' });
    // Intake has its count, "No limit" and its oldest wait, and no WIP meter: it is no station with slots.
    expect(within(queue).getByRole('heading', { level: 2, name: 'Intake' })).toBeTruthy();
    expect(within(queue).getByText('No limit')).toBeTruthy();
    expect(within(queue).queryByRole('img', { name: /in progress/ })).toBeNull();
    expect(within(queue).getByRole('link', { name: STANDARD })).toBeTruthy();

    expect(within(columns).getByRole('heading', { name: 'Investigate' })).toBeTruthy();
    expect(within(columns).getByRole('heading', { name: 'Build' })).toBeTruthy();
  });

  it('the header Needs you link leads to the Needs you page filtered to this factory', async () => {
    renderBoardPage({ page: ShellBoard, org, factory, board: board() });

    const link = await screen.findByRole('link', { name: /^Needs you/ });
    expect(link.getAttribute('href')).toBe('/o/acme/needs-you?factory=sre');
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
    const first = renderBoardPage({ page: ShellBoard, org, factory, board: board(), station: 'build' });
    expect(await screen.findByText('Build is the constraint: 1 of 1 busy, 0 waiting')).toBeTruthy();
    first.unmount();

    renderBoardPage({ page: ShellBoard, org, factory, board: board(), station: 'investigate' });
    await screen.findByRole('tabpanel');
    expect(screen.queryByText(/^Build is the constraint: /)).toBeNull();
  });

  it('keeps the phone status line to one line with the touches per item and a 44 px link to them', async () => {
    stubPhone();
    const touched = board({ touches: { finished: 4, per_item_mean: 1.4, per_item_median: 1, touchless_rate: 0.5 } });
    renderBoardPage({ page: ShellBoard, org, factory, board: touched });

    const line = await screen.findByTitle('Line running, WIP 2 of 3, 1.4 touches per item');
    expect(line.classList).toContain('truncate');
    expect(line.textContent).toContain('1.4 touches per item');

    const link = screen.getByRole('link', { name: 'By station' });
    expect(link.classList).toContain('min-h-11');
    expect(line.contains(link)).toBe(false);
  });

  it('shows no touches link while no item finished this week', async () => {
    stubPhone();
    renderBoardPage({ page: ShellBoard, org, factory, board: board() });

    await screen.findByTitle('Line running, WIP 2 of 3');
    expect(screen.queryByRole('link', { name: 'By station' })).toBeNull();
  });

  it('a done card shows its ref, age, title, PR link, touches and Move, but no summary and no state line', async () => {
    const REDIS = 'Redis evictions';
    const done = item({
      id: 'item-30',
      ref: 'sre#30',
      number: 30,
      title: REDIS,
      station: 'done',
      state: 'done',
      age_seconds: 7200,
      pr_url: 'https://github.com/acme/sre/pull/30',
      touch_count: 2,
      summary: 'Postmortem filed',
    });
    const withDone: Board = {
      ...board(),
      stations: [...board().stations, station({ id: 'done', kind: 'done', items: [done], done_today: 1 })],
    };
    const expectDoneCard = (card: HTMLElement) => {
      // The phone's list row joins the ref, type and age on one line; the columns split them.
      expect(within(card).getByText(/sre#30/)).toBeTruthy();
      expect(within(card).getByText(/2 h/)).toBeTruthy();
      expect(within(card).getByRole('link', { name: REDIS })).toBeTruthy();
      expect(
        within(card)
          .getByRole('link', { name: /opens in a new tab/ })
          .getAttribute('href'),
      ).toBe('https://github.com/acme/sre/pull/30');
      expect(within(card).getByText('2 touches')).toBeTruthy();
      expect(within(card).getByRole('button', { name: 'Move sre#30 to another station' })).toBeTruthy();
      expect(within(card).queryByText('Postmortem filed')).toBeNull();
      expect(within(card).queryByText('Done')).toBeNull();
    };

    const first = renderBoardPage({ page: ShellBoard, org, factory, board: withDone });
    const columns = await screen.findByRole('region', { name: 'Stations' });
    expectDoneCard(within(within(columns).getByRole('region', { name: 'Done' })).getByRole('article'));
    first.unmount();

    renderBoardPage({ page: ShellBoard, org, factory, board: withDone, station: 'done' });
    const panel = await screen.findByRole('tabpanel');
    expectDoneCard(within(panel).getByRole('article'));
  });
});

describe('the desktop Board, as the screen draws it', () => {
  it('heads the page Board, with the line status, the search and the view, and New item in the top bar', async () => {
    const creator = { ...factory, can: [...factory.can, 'item:create'] } as Factory;
    renderBoardPage({ page: ShellBoard, org, factory: creator, board: board() });

    await screen.findByRole('region', { name: 'Stations' });
    expect(screen.getByRole('heading', { level: 1, name: 'Board' })).toBeTruthy();
    expect(screen.getByText('Line running')).toBeTruthy();
    expect(screen.getByRole('searchbox', { name: 'Search items' })).toBeTruthy();
    expect((screen.getByRole('radio', { name: 'Board' }) as HTMLInputElement).checked).toBe(true);
    expect(screen.getByRole('button', { name: 'New item' })).toBeTruthy();
  });

  it('writes the search to the URL, and List goes to the Items page', async () => {
    const { router } = renderBoardPage({ page: ShellBoard, org, factory, board: board() });

    await screen.findByRole('region', { name: 'Stations' });
    fireEvent.change(screen.getByRole('searchbox', { name: 'Search items' }), { target: { value: 'Triage' } });
    await waitFor(() => expect((router.state.location.search as Record<string, unknown>).q).toBe('Triage'));
    expect(screen.queryByText(STANDARD)).toBeTruthy();
    expect(screen.queryByText(BUILD)).toBeNull();

    fireEvent.click(screen.getByRole('radio', { name: 'List' }));
    await waitFor(() => expect(router.state.location.pathname).toMatch(/\/items\/?$/));
  });

  it('writes the work type and the lane to the URL, and the select keeps the choice', async () => {
    const lanes = board({}, [expediteItem, { ...standardItem, lane: 'backend' }]);
    const { router } = renderBoardPage({ page: ShellBoard, org, factory, board: lanes });

    await screen.findByRole('region', { name: 'Stations' });
    const search = () => router.state.location.search as Record<string, unknown>;
    fireEvent.change(screen.getByRole('combobox', { name: 'Work type' }), { target: { value: 'incident' } });
    await waitFor(() => expect(search().type).toBe('incident'));
    fireEvent.change(screen.getByRole('combobox', { name: 'Lane' }), { target: { value: 'backend' } });
    await waitFor(() => expect(search().lane).toBe('backend'));
    expect(search().type).toBe('incident');
    expect((screen.getByRole('combobox', { name: 'Lane' }) as HTMLSelectElement).value).toBe('backend');
  });

  it('heads the Done column with Merged pull requests and the week’s count', async () => {
    const done = board();
    done.stations.push(station({ id: 'done', kind: 'done', items: [] }));
    renderBoardPage({
      page: ShellBoard,
      org,
      factory,
      board: {
        ...done,
        line: { ...done.line, touches: { finished: 53, per_item_mean: 0, per_item_median: 0, touchless_rate: 0 } },
      },
    });

    const columns = await screen.findByRole('region', { name: 'Stations' });
    const doneColumn = within(columns).getByRole('region', { name: 'Done' });
    expect(within(doneColumn).getByText('Merged pull requests')).toBeTruthy();
    expect(within(doneColumn).getByText('53 this week')).toBeTruthy();
  });

  it('shows the drum’s Ready buffer with Next in, and In progress below it, with the constraint chip', async () => {
    const ready = item({
      id: 'item-11',
      ref: 'sre#11',
      number: 11,
      title: 'Wire the retry flag',
      station: 'build',
      state: 'queued',
      in_buffer: true,
    });
    const drum = board({ release: { policy: 'push', enforced: false, drum: 'build', buffer: 2, in_buffer: 1 } });
    drum.stations = drum.stations.map((s) => (s.id === 'build' ? { ...s, items: [ready, buildItem] } : s));
    renderBoardPage({ page: ShellBoard, org, factory, board: drum });

    const columns = await screen.findByRole('region', { name: 'Stations' });
    const build = within(columns).getByRole('region', { name: 'Build' });
    expect(within(build).getByText('Constraint')).toBeTruthy();
    expect(within(build).getByText('Ready, buffer 1 of 2')).toBeTruthy();
    expect(within(build).getByText('Next in')).toBeTruthy();
    expect(within(build).getByText('In progress')).toBeTruthy();
    expect(within(build).getByRole('img', { name: '1 of 1 in progress' })).toBeTruthy();
    expect(within(build).getByRole('link', { name: 'Wire the retry flag' })).toBeTruthy();
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
      assemblies: [],
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
  /** The titles of the cards shown, in order: each card's heading link (not its assembly line). */
  const shownTitles = (region: HTMLElement = screen.getByRole('region', { name: 'Stations' })) =>
    within(region)
      .queryAllByRole('article')
      .map((card) => within(within(card).getByRole('heading', { level: 3 })).getByRole('link').textContent);

  /** The shown cards are the expected ones, whatever station each sits in. */
  const expectShown = (titles: string[], region?: HTMLElement) =>
    expect([...shownTitles(region)].sort()).toEqual([...titles].sort());

  /** A filter's chosen value: the option's value, '' for Any. */
  const choice = (name: string) => (screen.getByRole('combobox', { name }) as HTMLSelectElement).value;

  it('filters the cards by assembly, agent role and status, in every station', async () => {
    renderBoardPage({ page: ShellBoard, org, factory, board: filterBoard() });
    await screen.findByRole('region', { name: 'Stations' });
    const filter = async (name: string, value: string, titles: string[]) => {
      fireEvent.change(screen.getByRole('combobox', { name }), { target: { value } });
      await waitFor(() => expectShown(titles));
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

  it('says Empty in a column the filter leaves no card in, and keeps the column', async () => {
    renderBoardPage({ page: ShellBoard, org, factory, board: filterBoard() });
    await screen.findByRole('region', { name: 'Stations' });

    fireEvent.change(screen.getByRole('combobox', { name: 'Agent role' }), { target: { value: 'Triager' } });
    await waitFor(() => expectShown([ROTATE]));
    const build = within(screen.getByRole('region', { name: 'Stations' })).getByRole('region', { name: 'Build' });
    expect(within(build).getByRole('heading', { name: 'Build' })).toBeTruthy();
    expect(within(build).getByText('Empty')).toBeTruthy();
  });

  it('orders a station’s cards by assembly when grouped, and keeps the order of the board otherwise', async () => {
    const grouped = renderBoardPage({
      page: ShellBoard,
      org,
      factory,
      board: filterBoard(),
      search: { group: 'assembly' },
    });
    const investigate = await screen.findByRole('region', { name: 'Investigate' });
    // Each assembly's parts together in part order, the assemblies by their first part, then the loose cards.
    expect(shownTitles(investigate)).toEqual([DRAFT, ADD, WIRE, TRIAGE, ROTATE]);
    grouped.unmount();

    renderBoardPage({ page: ShellBoard, org, factory, board: filterBoard() });
    const ungrouped = await screen.findByRole('region', { name: 'Investigate' });
    expect(shownTitles(ungrouped)).toEqual([DRAFT, TRIAGE, WIRE, ADD, ROTATE]);
  });

  it('shows the view its search params name, and writes each change back to the URL', async () => {
    const { router } = renderBoardPage({
      page: ShellBoard,
      org,
      factory,
      board: filterBoard(),
      search: { assembly: ASM1, group: 'assembly' },
    });
    await screen.findByRole('region', { name: 'Stations' });
    const search = () => router.state.location.search as Record<string, unknown>;
    expect(choice('Assembly')).toBe(ASM1);
    expectShown([ADD, WIRE]);

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
      page: ShellBoard,
      org,
      factory,
      board: filterBoard(),
      search: { assembly: 'item-gone', role: 'Infra engineer', status: 'blocked', group: 'lane', type: 'nope' },
    });
    await screen.findByRole('region', { name: 'Stations' });
    expect(choice('Assembly')).toBe('');
    expect(choice('Agent role')).toBe('');
    expect(choice('Status')).toBe('');
    expect(choice('Work type')).toBe('');
    expect(choice('Lane')).toBe('');
    expect(screen.getByRole('searchbox', { name: 'Search items' })).toHaveProperty('value', '');
    expect(screen.getByRole('radio', { name: 'None' })).toHaveProperty('checked', true);
    expectShown([ADD, DRAFT, TRIAGE, WIRE, ROTATE, SHIP]);
  });

  /** An open assembly on the board: "PWA" (sre#50), 3 of its 5 parts merged; its own card is in no station. */
  const PWA = item({
    id: 'item-50',
    ref: 'sre#50',
    title: 'PWA',
    state: 'assembly',
    parts: { total: 5, merged: 3, in_progress: 1, waiting: 1, stations: ['investigate'] },
  });
  const pwaPartOf = (n: number) => ({ id: 'item-50', ref: 'sre#50', title: 'PWA', part: n, parts: 5 });

  it('shows an open assembly on no station column, and each part’s card with its line and one pip per part', async () => {
    const parts = [
      item({
        id: 'item-51',
        ref: 'sre#51',
        title: ADD,
        state: 'in_progress',
        agent: 'investigator',
        slot: 1,
        turn: 2,
        board_lane: 'investigator',
        assembly: pwaPartOf(4),
      }),
      item({ id: 'item-52', ref: 'sre#52', title: WIRE, board_lane: 'investigator', assembly: pwaPartOf(5) }),
    ];
    const board: Board = {
      ...filterBoard(),
      stations: [station({ id: 'investigate', lanes: [lane({})], items: parts, in_progress: 1, wip_limit: 2 })],
      assemblies: [PWA],
    };
    renderBoardPage({ page: ShellBoard, org, factory, board, search: { group: 'assembly' } });
    const columns = await screen.findByRole('region', { name: 'Stations' });

    // The assembly is no card and no header: its parts are the cards, in part order.
    expect(within(columns).queryAllByRole('link', { name: /merged/ })).toHaveLength(0);
    expectShown([ADD, WIRE]);
    const [addCard, wireCard] = within(columns).getAllByRole('article');
    expect(within(addCard as HTMLElement).getByRole('link', { name: 'Part 4 of 5 of PWA' })).toBeTruthy();
    expect(within(wireCard as HTMLElement).getByRole('link', { name: 'Part 5 of 5 of PWA' })).toBeTruthy();
    // Three of the five parts are merged: three filled pips on each part's card.
    for (const card of [addCard, wireCard] as HTMLElement[]) {
      expect(card.querySelectorAll('i.bg-ok')).toHaveLength(3);
      expect(card.querySelectorAll('i.bg-ok-tint')).toHaveLength(2);
    }
  });

  it('shows a done assembly as a normal card in the Done column', async () => {
    const done = item({ id: 'item-60', ref: 'sre#60', title: 'Billing migration', state: 'done', station: 'done' });
    const board: Board = {
      ...filterBoard(),
      stations: [
        station({ id: 'investigate', items: [] }),
        station({ id: 'done', kind: 'done', items: [done], done_today: 1 }),
      ],
      assemblies: [],
    };
    renderBoardPage({ page: ShellBoard, org, factory, board, search: { group: 'assembly' } });
    const columns = await screen.findByRole('region', { name: 'Stations' });
    expect(shownTitles(within(columns).getByRole('region', { name: 'Done' }))).toEqual(['Billing migration']);
    expect(within(columns).queryAllByRole('link', { name: /merged/ })).toHaveLength(0);
  });

  it('shows the same header over the grouped parts in the phone’s picked station', async () => {
    const part = item({
      id: 'item-52',
      ref: 'sre#52',
      title: WIRE,
      board_lane: 'investigator',
      assembly: pwaPartOf(5),
    });
    const board: Board = {
      ...filterBoard(),
      stations: [station({ id: 'investigate', lanes: [lane({})], items: [part], in_progress: 0, wip_limit: 2 })],
      assemblies: [PWA],
    };
    renderBoardPage({ page: ShellBoard, org, factory, board, search: { group: 'assembly', station: 'investigate' } });
    const panel = await screen.findByRole('tabpanel');
    const header = within(panel).getByRole('link', { name: /3 of 5 merged/ });
    expect(header.getAttribute('href')).toBe('/o/acme/f/sre/items/50');
    expect(shownTitles(panel)).toEqual([WIRE]);
  });
});
