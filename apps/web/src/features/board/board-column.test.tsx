// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Board, BoardStation, Factory, ItemCard, OrgSummary } from '../../api/types';
import { ShellFrame } from '../../app/app-shell';
import { renderBoardPage } from '../../test/route';
import { TooltipProvider } from '../../ui/tooltip';
import { BoardPage } from './board-page';
import { columnLimit } from './stats';

const ShellBoard = () => (
  <TooltipProvider>
    <ShellFrame>
      <BoardPage />
    </ShellFrame>
  </TooltipProvider>
);

vi.mock(import('../../api/client'), async (original) => ({
  ...(await original()),
  getInbox: vi.fn(async () => ({ entries: [], truncated: false })),
}));

afterEach(() => {
  cleanup();
  // @ts-expect-error restoring the jsdom default, which has no matchMedia
  delete window.matchMedia;
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: null };
const factory: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'sre',
  title: 'SRE',
  work_types: ['task'],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'approver',
  can: ['item:operate', 'item:decide'],
};

const item = (n: number, over: Partial<ItemCard> = {}): ItemCard => ({
  id: `item-${n}`,
  ref: `sre#${n}`,
  number: n,
  title: `Card ${n}`,
  type: 'task',
  station: 'build',
  state: 'in_progress',
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

const buildStation = (items: ItemCard[]): BoardStation => ({
  id: 'build',
  kind: 'agent',
  agents: ['builder'],
  wip_limit: 6,
  waiting: 0,
  in_progress: items.filter((it) => !it.in_buffer).length,
  items,
  busy_ratio: null,
  send_back_ratio: null,
  queue_trend: null,
  oldest_waiting_seconds: null,
  done_today: null,
});

/** One station, Build, which is the drum with a buffer of two. */
const board = (items: ItemCard[]): Board => ({
  factory: { id: factory.id, name: factory.name, title: 'SRE' },
  stations: [buildStation(items)],
  assemblies: [],
  constraint: 'build',
  constraint_source: 'configured',
  line: {
    status: 'running',
    andon: null,
    wip: { in_progress: 1, limit: 6 },
    release: { policy: 'push', enforced: false, drum: 'build', buffer: 2, in_buffer: 1 },
    expedite: null,
    budget: null,
    touches: { finished: 0, per_item_mean: 0, per_item_median: 0, touchless_rate: 0 },
  },
});

function stubPhone() {
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: true,
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  }));
}

/** The titles of the column's cards in page order. */
const titles = (column: HTMLElement) =>
  within(column)
    .getAllByRole('article')
    .map((a) => a.querySelector('a')?.textContent);

async function buildColumn() {
  const columns = await screen.findByRole('region', { name: 'Stations' });
  return within(columns).getByRole('region', { name: 'Build' });
}

describe('the Done column header', () => {
  it('shows both figures in full and lets the meta line wrap instead of clipping one', async () => {
    const done: BoardStation = {
      ...buildStation([]),
      id: 'done',
      kind: 'done',
      agents: [],
      wip_limit: null,
      in_progress: 0,
      done_today: 0,
    };
    const base = board([]);
    renderBoardPage({
      page: ShellBoard,
      org,
      factory,
      board: {
        ...base,
        stations: [done],
        constraint: null,
        line: { ...base.line, touches: { ...base.line.touches, finished: 281 } },
      },
    });
    const columns = await screen.findByRole('region', { name: 'Stations' });
    const column = within(columns).getByRole('region', { name: 'Done' });

    expect(within(column).getByText('0 today')).toBeTruthy();
    const week = within(column).getByText('281 this week');
    expect(within(column).getByText('Merged pull requests')).toBeTruthy();
    expect(week.classList).not.toContain('shrink-0');
    expect(week.parentElement?.classList).toContain('flex-wrap');
  });
});

describe('a column with a buffer', () => {
  // The Ready card arrives first from the server; the column still lists In progress first.
  const items = [item(1, { state: 'queued', in_buffer: true }), item(2), item(3)];

  it('lists the In progress cards first, then the dimmed Ready cards under their label', async () => {
    renderBoardPage({ page: ShellBoard, org, factory, board: board(items) });
    const build = await buildColumn();

    expect(titles(build)).toEqual(['Card 2', 'Card 3', 'Card 1']);
    const label = within(build).getByText('Ready, buffer 1 of 2');
    expect(within(build).getByText('In progress').compareDocumentPosition(label)).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING,
    );
    const ready = within(build).getByRole('link', { name: 'Card 1' }).closest('[data-group="ready"]');
    expect([...(ready?.classList ?? [])].some((c) => c.startsWith('opacity-'))).toBe(false);
    expect(within(ready as HTMLElement).getByRole('article').classList).toContain('bg-panel');
    expect(within(build).getByRole('link', { name: 'Card 2' }).closest('article')?.classList).toContain('bg-card');
  });

  it('keeps the same order on a phone', async () => {
    stubPhone();
    renderBoardPage({ page: ShellBoard, org, factory, board: board(items), search: { station: 'build' } });
    const [build] = await screen.findAllByRole('region', { name: 'Build' });
    expect(titles(build as HTMLElement)).toEqual(['Card 2', 'Card 3', 'Card 1']);
  });
});

describe('the constraint mark and the zone labels', () => {
  const items = [item(1, { state: 'queued', in_buffer: true }), item(2)];

  it('marks only the drum’s column with a square yellow mark and Constraint, and labels only its zones', async () => {
    const withTwo = board(items);
    withTwo.stations = [{ ...withTwo.stations[0], id: 'spec', items: [item(5)] } as BoardStation, ...withTwo.stations];
    renderBoardPage({ page: ShellBoard, org, factory, board: withTwo });
    const build = await buildColumn();
    const spec = within(screen.getByRole('region', { name: 'Stations' })).getByRole('region', { name: 'Spec' });

    expect(within(build).getByText('Constraint')).toBeTruthy();
    const mark = within(build).getByText('Constraint').querySelector('i');
    expect(mark?.className).toContain('bg-amber');
    expect(mark?.className).not.toContain('rounded-chip');
    expect(within(spec).queryByText('Constraint')).toBeNull();
    expect(within(spec).queryByText('In progress')).toBeNull();
    expect(within(spec).queryByText(/^Ready, buffer/)).toBeNull();
  });

  it('lets the drum’s zone label shrink to its column, with a title, and keeps "Next in" whole', async () => {
    renderBoardPage({ page: ShellBoard, org, factory, board: board(items) });
    const build = await buildColumn();
    const label = within(build).getByText(/^Ready, buffer/);
    expect(label.className).toContain('min-w-0');
    expect(label.className).toContain('truncate');
    expect(label.getAttribute('title')).toBe(label.textContent);
    const next = within(build).getByText('Next in');
    expect(next.className).toContain('shrink-0');
    expect(next.className).toContain('whitespace-nowrap');
  });

  it('on a phone, the drum’s tab is marked and its column holds In progress, then Ready, buffer', async () => {
    stubPhone();
    renderBoardPage({ page: ShellBoard, org, factory, board: board(items), search: { station: 'build' } });
    const [build] = await screen.findAllByRole('region', { name: 'Build' });
    const text = (build as HTMLElement).textContent ?? '';
    expect(text.indexOf('In progress')).toBeGreaterThanOrEqual(0);
    expect(text.indexOf('In progress')).toBeLessThan(text.indexOf('Ready, buffer 1 of 2'));
    expect(within(build as HTMLElement).getByText('Constraint')).toBeTruthy();
  });
});

describe('Show N more', () => {
  const many = Array.from({ length: columnLimit + 3 }, (_, i) => item(i + 10));
  const ready = item(1, { state: 'queued', in_buffer: true });

  it('shows the first cards, then reveals the rest', async () => {
    renderBoardPage({ page: ShellBoard, org, factory, board: board([...many, ready]) });
    const build = await buildColumn();

    expect(within(build).getAllByRole('article')).toHaveLength(columnLimit + 1);
    fireEvent.click(within(build).getByRole('button', { name: 'Show 3 more' }));
    expect(within(build).getAllByRole('article')).toHaveLength(columnLimit + 4);
    expect(within(build).getByRole('button', { name: 'Show fewer' })).toBeTruthy();
  });

  it('sits at the end of the In progress group, above the Ready label', async () => {
    renderBoardPage({ page: ShellBoard, org, factory, board: board([...many, ready]) });
    const build = await buildColumn();

    const more = within(build).getByRole('button', { name: 'Show 3 more' });
    const label = within(build).getByText('Ready, buffer 1 of 2');
    expect(within(build).queryByRole('link', { name: `Card ${columnLimit + 10}` })).toBeNull();
    const lastShown = within(build)
      .getByRole('link', { name: `Card ${columnLimit + 9}` })
      .closest('article');
    expect(lastShown?.compareDocumentPosition(more)).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
    expect(more.compareDocumentPosition(label)).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
  });

  it('offers nothing when the cards fit', async () => {
    renderBoardPage({ page: ShellBoard, org, factory, board: board(many.slice(0, columnLimit)) });
    const build = await buildColumn();
    await waitFor(() => expect(within(build).getAllByRole('article')).toHaveLength(columnLimit));
    expect(within(build).queryByRole('button', { name: /Show/ })).toBeNull();
  });
});
