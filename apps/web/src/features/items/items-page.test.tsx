// @vitest-environment jsdom

import { ApiError } from '@goal/api-client';
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { toast } from 'sonner';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { listItems, moveItem, pauseItem, setItemLane } from '../../api/client';
import { boardQuery, versionModelQuery } from '../../api/queries';
import type { Board, Factory, Item, ItemCard, OrgSummary, Station } from '../../api/types';
import { renderFactoryPage } from '../../test/route';
import { TooltipProvider } from '../../ui/tooltip';
import { ItemsPage } from './items-page';

// The list reads the items, filtered by state like the server; the tests set what it holds, and
// which requests fail. The Move and Pause requests are mocked: each one is a call per item.
const list = vi.hoisted(() => ({
  items: [] as ItemCard[],
  fail: false,
  /** While set, the list never answers: the page stays in its loading state. */
  hold: false,
  refuse: {} as Record<string, string>,
  /** The problem type goald gives each item that the lane request refuses. */
  laneRefusal: {} as Record<string, string>,
}));

vi.mock(import('../../api/client'), async (original) => ({
  ...(await original()),
  listItems: vi.fn(async (_org: string, _factory: string, filter: { state?: string | undefined }) => {
    if (list.hold) return new Promise<never>(() => {});
    if (list.fail) throw new Error('The server is busy');
    return {
      items: filter.state ? list.items.filter((it) => it.state === filter.state) : list.items,
      next_after: null,
    };
  }),
  moveItem: vi.fn(async (_org: string, key: string, to: string) => {
    if (list.refuse[key]) throw new Error(list.refuse[key]);
    return { ...list.items.find((it) => it.ref === key), station: to } as unknown as Item;
  }),
  pauseItem: vi.fn(
    async (_org: string, key: string) =>
      ({ ...list.items.find((it) => it.ref === key), state: 'queued' }) as unknown as Item,
  ),
  setItemLane: vi.fn(async (_org: string, key: string, lane: string) => {
    const refusal = list.laneRefusal[key];
    if (refusal) throw new ApiError({ type: 'about:blank', title: 'Conflict', status: 409, code: refusal } as never);
    return { ...list.items.find((it) => it.ref === key), lane } as unknown as Item;
  }),
}));
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  list.fail = false;
  list.hold = false;
  list.refuse = {};
  list.laneRefusal = {};
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: null };
const stations = [
  { id: 'investigate', kind: 'agent' },
  { id: 'build', kind: 'agent' },
  { id: 'merge', kind: 'human' },
  { id: 'done', kind: 'done' },
] as unknown as Station[];
const factory: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'sre',
  title: 'SRE',
  work_types: ['incident'],
  stations,
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'approver',
  can: ['item:operate', 'item:decide'],
};

const now = () => new Date().toISOString();

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

const board: Board = {
  factory: { id: factory.id, name: factory.name, title: 'SRE' },
  stations: [],
  constraint: null,
  constraint_source: 'configured',
  line: {
    status: 'running',
    andon: null,
    wip: { in_progress: 0, limit: null },
    release: { policy: 'push', enforced: false, drum: null, buffer: null, in_buffer: 0 },
    expedite: null,
    budget: null,
    touches: { finished: 0, per_item_mean: 0, per_item_median: 0, touchless_rate: 0 },
  },
} as unknown as Board;

/** The status icons in a row: a StatusIcon is a shrink-0 svg. */
function statusIcons(row: HTMLElement): string[] {
  return [...row.querySelectorAll('svg.shrink-0')].map((svg) => svg.getAttribute('class') ?? '');
}

/** The table row that holds a title. */
async function rowOf(title: string): Promise<HTMLElement> {
  return (await screen.findByText(title)).closest('tr') as HTMLElement;
}

function renderItems(items: ItemCard[], search: Record<string, string> = {}, permissions = factory.can) {
  list.items = items;
  return renderFactoryPage({
    page: () => (
      <TooltipProvider>
        <ItemsPage />
      </TooltipProvider>
    ),
    path: 'items/',
    org,
    factory: { ...factory, can: permissions },
    search,
    seed: [
      [boardQuery(org.id, factory.id).queryKey, board],
      [versionModelQuery(org.id, factory.id, 'current').queryKey, { model: runningLine }],
    ],
  });
}

/** The running version's line: its build station routes two lanes, so Set lane has choices. */
const runningLine = {
  line: {
    stations: [
      { id: 'investigate', kind: 'agent', agent: 'investigator' },
      { id: 'build', kind: 'agent', route: { frontend: 'frontend-engineer', backend: 'backend-engineer' } },
    ],
  },
};

describe('Items page header and views', () => {
  it('shows the heading, the open and done counts, and the saved views with their counts', async () => {
    renderItems([
      item({ id: 'a', ref: 'sre#1', title: 'Open one', state: 'queued' }),
      item({ id: 'b', ref: 'sre#2', title: 'Waiting one', state: 'waiting_human' }),
      item({ id: 'c', ref: 'sre#3', title: 'Finished', state: 'done', updated_at: now() }),
    ]);

    expect(await screen.findByRole('heading', { level: 1, name: 'Items' })).toBeTruthy();
    expect(screen.getByText('2 open, 1 done this week')).toBeTruthy();
    const views = screen.getByRole('navigation', { name: 'Saved views' });
    expect(within(views).getByRole('link', { name: /^Open/ }).getAttribute('aria-current')).toBe('page');
    expect(within(views).getByRole('link', { name: /^Waiting on you/ }).textContent).toContain('1');
    expect(within(views).getByRole('link', { name: /^Done this week/ }).textContent).toContain('1');
  });

  it('marks the Waiting on you view as current when the URL has its state', async () => {
    renderItems(
      [
        item({ id: 'b', ref: 'sre#2', title: 'Waiting one', state: 'waiting_human' }),
        item({ id: 'a', title: 'Open one' }),
      ],
      { state: 'waiting_human' },
    );

    const views = await screen.findByRole('navigation', { name: 'Saved views' });
    expect(
      within(views)
        .getByRole('link', { name: /^Waiting on you/ })
        .getAttribute('aria-current'),
    ).toBe('page');
    expect(within(views).getByRole('link', { name: /^Open/ }).getAttribute('aria-current')).not.toBe('page');
    expect(screen.queryByText('Open one')).toBeNull();
    expect(screen.getByText('Waiting one')).toBeTruthy();
  });

  it('switches to a view from its tab, and the Expedite view keeps only expedite items', async () => {
    renderItems([
      item({ id: 'a', ref: 'sre#1', title: 'Normal work' }),
      item({ id: 'b', ref: 'sre#2', title: 'Urgent fix', class_of_service: 'expedite' }),
    ]);

    fireEvent.click(await screen.findByRole('link', { name: /^Expedite/ }));
    expect(await screen.findByRole('link', { name: /^Expedite/ })).toBeTruthy();
    expect(screen.queryByText('Normal work')).toBeNull();
    expect(screen.getByText('Urgent fix')).toBeTruthy();
  });

  it('lists only the items done in the last seven days under Done this week, and its count matches', async () => {
    renderItems([
      item({ id: 'a', ref: 'sre#1', title: 'Shipped today', state: 'done', updated_at: now() }),
      item({ id: 'b', ref: 'sre#2', title: 'Shipped long ago', state: 'done', updated_at: '2020-01-01T00:00:00Z' }),
    ]);

    fireEvent.click(await screen.findByRole('link', { name: /^Done this week/ }));
    const views = await screen.findByRole('navigation', { name: 'Saved views' });
    expect(
      within(views)
        .getByRole('link', { name: /^Done this week/ })
        .getAttribute('aria-current'),
    ).toBe('page');
    expect(within(views).getByRole('link', { name: /^Done this week/ }).textContent).toContain('1');
    expect(await screen.findByText('Shipped today')).toBeTruthy();
    expect(screen.queryByText('Shipped long ago')).toBeNull();
  });

  it('renders Source as not available, and opens the save dialog from Save view', async () => {
    renderItems([item({ id: 'a', ref: 'sre#1', title: 'Some work' })]);

    fireEvent.click(await screen.findByRole('button', { name: '+ Save view' }));
    expect(await screen.findByRole('dialog', { name: 'Save view' })).toBeTruthy();
    fireEvent.keyDown(screen.getByRole('button', { name: 'Cancel' }), { key: 'Escape' });
    const source = screen.getByRole('button', { name: /^Source/ });
    expect(source.getAttribute('aria-disabled')).toBe('true');
    fireEvent.click(source);
    expect(screen.getByText('Some work')).toBeTruthy();
    expect(screen.queryByRole('region', { name: 'Bulk actions' })).toBeNull();
  });
});

describe('Items filters in the URL', () => {
  it('filters by type, lane and assembly from the URL and names the chosen value on the button', async () => {
    renderItems(
      [
        item({ id: 'a', ref: 'sre#1', title: 'Backend bug', type: 'incident', lane: 'backend' }),
        item({ id: 'b', ref: 'sre#2', title: 'Frontend task', type: 'task', lane: 'frontend' }),
      ],
      { type: 'incident', lane: 'backend' },
    );

    expect(await screen.findByText('Backend bug')).toBeTruthy();
    expect(screen.queryByText('Frontend task')).toBeNull();
    expect(screen.getByRole('button', { name: 'Type: Incident' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Lane: backend' })).toBeTruthy();
  });

  it('applies a state filter to the parts of an assembly, under the assembly progress', async () => {
    renderItems(
      [
        assembly(),
        partOf(1, { title: 'Move the schema', state: 'in_progress', agent: 'infra-engineer' }),
        partOf(2, { title: 'Backfill the ledger', state: 'queued' }),
        item({ id: 'plain', title: 'Checkout API latency', state: 'in_progress' }),
      ],
      { state: 'in_progress' },
    );

    expect(await screen.findByText('Move the schema')).toBeTruthy();
    expect(screen.getByText('1 of 3 parts merged')).toBeTruthy();
    expect(screen.getByText('Checkout API latency')).toBeTruthy();
    expect(screen.queryByText('Backfill the ledger')).toBeNull();
  });

  it('keeps the Show choice across a view change and lists every item flat when it is off', async () => {
    renderItems([assembly(), partOf(1, { title: 'Move the schema', state: 'done', updated_at: now() })], {
      show: 'flat',
    });

    const grouped = await screen.findByRole('switch', { name: 'Grouped by assembly' });
    expect(grouped.getAttribute('aria-checked')).toBe('false');
    expect(screen.queryByRole('button', { name: 'Parts of Migrate billing' })).toBeNull();
    expect(screen.getAllByRole('row')).toHaveLength(3);

    fireEvent.click(screen.getByRole('link', { name: /^Done this week/ }));
    expect((await screen.findByRole('switch', { name: 'Grouped by assembly' })).getAttribute('aria-checked')).toBe(
      'false',
    );
  });
});

describe('Items list states', () => {
  it('shows the app background with aria-busy and a spoken Loading while the items load', async () => {
    list.hold = true;
    renderItems([]);

    expect((await screen.findByText('Loading')).closest('main')?.getAttribute('aria-busy')).toBe('true');
  });

  it('shows an empty sentence and New item when the factory has no items', async () => {
    renderItems([]);

    expect(await screen.findByText('No items here. Create one with New item.')).toBeTruthy();
  });

  it('shows the error with a Retry button when the list fails to load', async () => {
    list.fail = true;
    renderItems([item({ title: 'Never shown' })]);

    expect(await screen.findByRole('alert')).toBeTruthy();
    expect(screen.getByText(/Could not load the items\. The server is busy/)).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Retry' })).toBeTruthy();
  });
});

/** A part of the assembly "Migrate billing" (sre#10), three parts in all. */
const partOf = (part: number, over: Partial<ItemCard>): ItemCard =>
  item({
    id: `part-${part}`,
    ref: `sre#${10 + part}`,
    number: 10 + part,
    title: `Part ${part} title`,
    assembly: { id: 'asm-1', ref: 'sre#10', title: 'Migrate billing', part, parts: 3 },
    ...over,
  });

const assembly = (over: Partial<ItemCard> = {}): ItemCard =>
  item({
    id: 'asm-1',
    ref: 'sre#10',
    number: 10,
    title: 'Migrate billing',
    state: 'assembly',
    parts: { total: 3, merged: 1, in_progress: 1, waiting: 1, stations: ['build', 'merge'] },
    ...over,
  });

describe('Items list status icons', () => {
  it('shows the working icon for an item in progress', async () => {
    renderItems([item({ title: 'Checkout API latency', state: 'in_progress', agent: 'infra-engineer' })]);

    const row = await rowOf('Checkout API latency');
    expect(within(row).getAllByText('Infra engineer working').length).toBeGreaterThan(0);
    const icons = statusIcons(row);
    expect(icons.length).toBeGreaterThan(0);
    expect(icons.every((c) => c.includes('text-run'))).toBe(true);
  });

  it('shows the clock for a queued item', async () => {
    renderItems([item({ title: 'Triage the export', state: 'queued' })]);

    const icons = statusIcons(await rowOf('Triage the export'));
    expect(icons.length).toBeGreaterThan(0);
    expect(icons.every((c) => c.includes('text-amber'))).toBe(true);
  });

  it('shows the check for a done item', async () => {
    renderItems([item({ title: 'Rotate the keys', state: 'done', summary: 'Keys rotated' })]);

    const icons = statusIcons(await rowOf('Rotate the keys'));
    expect(icons.length).toBeGreaterThan(0);
    expect(icons.every((c) => c.includes('text-ok'))).toBe(true);
  });

  it('shows no icon for a blocked item, whose words say it', async () => {
    renderItems([item({ title: 'Deploy the parser', state: 'blocked' })]);

    const row = await rowOf('Deploy the parser');
    expect(within(row).getAllByText('Blocked, needs a retry').length).toBeGreaterThan(0);
    expect(statusIcons(row)).toEqual([]);
  });

  it('gives the state words their full text in a title attribute', async () => {
    renderItems([item({ title: 'Triage the export', state: 'queued' })]);

    const states = (await rowOf('Triage the export')).querySelectorAll('[title="Waiting"]');
    expect(states.length).toBeGreaterThan(0);
  });

  it('shows an expedite item with its Expedite chip beside its state', async () => {
    renderItems([item({ title: 'Fix the lease', class_of_service: 'expedite', state: 'in_progress' })]);

    expect(within(await rowOf('Fix the lease')).getByText('Expedite')).toBeTruthy();
  });
});

describe('Items list assemblies', () => {
  it('shows an assembly as one group row with its parts beneath it, in plan order', async () => {
    renderItems([
      assembly(),
      partOf(2, { title: 'Backfill the ledger', state: 'queued', station: 'build' }),
      partOf(1, { title: 'Move the schema', state: 'done', station: 'merge' }),
      item({ id: 'plain', title: 'Triage the export', type: 'incident', state: 'queued' }),
    ]);

    const toggle = await screen.findByRole('button', { name: 'Parts of Migrate billing' });
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByText('1 of 3 parts merged')).toBeTruthy();
    expect(screen.getByText('Build, Merge')).toBeTruthy();
    expect(screen.getAllByTitle('Migrate billing').length).toBeGreaterThan(0);

    const titles = [...document.querySelectorAll('tbody a')].map((a) => a.textContent ?? '');
    expect(titles.findIndex((t) => t.includes('Move the schema'))).toBeLessThan(
      titles.findIndex((t) => t.includes('Backfill the ledger')),
    );
    // A part never also shows as a top-level row: its title is in one link only.
    expect(screen.getAllByText('Move the schema')).toHaveLength(1);
    expect(screen.getByText('Triage the export')).toBeTruthy();
  });

  it('shows a part with its station and its status in the shared words and icon', async () => {
    renderItems([assembly(), partOf(1, { title: 'Move the schema', state: 'done', station: 'merge' })]);

    const row = await rowOf('Move the schema');
    expect(within(row).getAllByText('Merge').length).toBeGreaterThan(0);
    expect(within(row).getAllByText('Done').length).toBeGreaterThan(0);
    const icons = statusIcons(row);
    expect(icons.length).toBeGreaterThan(0);
    expect(icons.every((c) => c.includes('text-ok'))).toBe(true);
  });

  it('collapses and expands its parts', async () => {
    renderItems([assembly(), partOf(1, { title: 'Move the schema', state: 'done' })]);

    const toggle = await screen.findByRole('button', { name: 'Parts of Migrate billing' });
    fireEvent.click(toggle);
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    expect(screen.queryByText('Move the schema')).toBeNull();
    fireEvent.click(toggle);
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByText('Move the schema')).toBeTruthy();
  });

  it('builds an assembly row from a part when the assembly is not on the page, without a progress bar', async () => {
    renderItems([partOf(2, { title: 'Backfill the ledger', state: 'queued' })]);

    expect(await screen.findByRole('button', { name: 'Parts of Migrate billing' })).toBeTruthy();
    expect(screen.getByText('3 parts')).toBeTruthy();
    expect(screen.queryByText(/parts merged/)).toBeNull();
    expect(screen.getByText('Backfill the ledger')).toBeTruthy();
  });

  it('footnotes how many items the table shows', async () => {
    renderItems([assembly(), partOf(1, { title: 'Move the schema', state: 'done' })]);

    expect(await screen.findByText(/^Showing 1 of 2 items\./)).toBeTruthy();
  });
});

/** Two queued items at the first station, for the bulk actions. */
const twoItems = () => [
  item({ id: 'one', ref: 'sre#1', title: 'First item' }),
  item({ id: 'two', ref: 'sre#2', title: 'Second item' }),
];

describe('Items list bulk actions', () => {
  it('shows the bulk actions for the checked rows and clears them', async () => {
    renderItems(twoItems());

    fireEvent.click(await screen.findByRole('checkbox', { name: 'Select sre#1' }));
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select sre#2' }));
    const bar = await screen.findByRole('region', { name: 'Bulk actions' });
    expect(within(bar).getByText('2 selected')).toBeTruthy();
    expect(within(bar).getByRole('button', { name: 'Move' })).toBeTruthy();
    expect(within(bar).getByRole('button', { name: 'Pause' })).toBeTruthy();
    expect(within(bar).getByRole('button', { name: 'Set lane' }).getAttribute('aria-disabled')).toBeNull();

    fireEvent.click(within(bar).getByRole('button', { name: 'Clear' }));
    await waitFor(() => expect(screen.queryByRole('region', { name: 'Bulk actions' })).toBeNull());
  });

  it('pauses each checked item with its own request', async () => {
    renderItems(twoItems());

    fireEvent.click(await screen.findByRole('checkbox', { name: 'Select sre#1' }));
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select sre#2' }));
    fireEvent.click(
      within(await screen.findByRole('region', { name: 'Bulk actions' })).getByRole('button', { name: 'Pause' }),
    );

    await waitFor(() => expect(pauseItem).toHaveBeenCalledTimes(2));
    expect(pauseItem).toHaveBeenCalledWith('org-1', 'sre#1');
    expect(pauseItem).toHaveBeenCalledWith('org-1', 'sre#2');
  });

  it('moves each checked item to the station the Move menu chose', async () => {
    renderItems(twoItems());

    fireEvent.click(await screen.findByRole('checkbox', { name: 'Select sre#1' }));
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select sre#2' }));
    fireEvent.click(
      within(await screen.findByRole('region', { name: 'Bulk actions' })).getByRole('button', { name: 'Move' }),
    );
    fireEvent.click(await screen.findByRole('radio', { name: /Build/ }));
    fireEvent.click(screen.getByRole('button', { name: 'Move to Build' }));

    await waitFor(() => expect(moveItem).toHaveBeenCalledTimes(2));
    expect(moveItem).toHaveBeenCalledWith('org-1', 'sre#1', 'build', undefined);
    expect(moveItem).toHaveBeenCalledWith('org-1', 'sre#2', 'build', undefined);
  });

  it('names a refused item and still moves the others', async () => {
    list.refuse = { 'sre#2': 'The station is closed' };
    renderItems(twoItems());

    fireEvent.click(await screen.findByRole('checkbox', { name: 'Select sre#1' }));
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select sre#2' }));
    fireEvent.click(
      within(await screen.findByRole('region', { name: 'Bulk actions' })).getByRole('button', { name: 'Move' }),
    );
    fireEvent.click(await screen.findByRole('radio', { name: /Build/ }));
    fireEvent.click(screen.getByRole('button', { name: 'Move to Build' }));

    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith('Could not move sre#2', { description: 'The station is closed' }),
    );
    expect(toast.success).toHaveBeenCalledWith('Moved sre#1 to Build.');
  });

  it('hides Move and Pause, and keeps Clear, without the permission to operate items', async () => {
    renderItems(twoItems(), {}, ['item:decide']);

    fireEvent.click(await screen.findByRole('checkbox', { name: 'Select sre#1' }));
    const bar = await screen.findByRole('region', { name: 'Bulk actions' });
    expect(within(bar).queryByRole('button', { name: 'Move' })).toBeNull();
    expect(within(bar).queryByRole('button', { name: 'Pause' })).toBeNull();
    expect(within(bar).queryByRole('button', { name: 'Set lane' })).toBeNull();
    expect(within(bar).getByRole('button', { name: 'Clear' })).toBeTruthy();
  });

  it('sets the lane of each checked item and reports a refusal in progress, the others still set', async () => {
    list.laneRefusal = { 'sre#2': 'item-in-progress' };
    renderItems(twoItems());

    fireEvent.click(await screen.findByRole('checkbox', { name: 'Select sre#1' }));
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select sre#2' }));
    fireEvent.click(
      within(await screen.findByRole('region', { name: 'Bulk actions' })).getByRole('button', { name: 'Set lane' }),
    );
    fireEvent.click(await screen.findByRole('radio', { name: 'Frontend' }));
    const listsBefore = vi.mocked(listItems).mock.calls.length;
    fireEvent.click(screen.getByRole('button', { name: 'Set lane to Frontend' }));

    await waitFor(() => expect(setItemLane).toHaveBeenCalledTimes(2));
    expect(setItemLane).toHaveBeenCalledWith('org-1', 'sre#1', 'frontend');
    expect(setItemLane).toHaveBeenCalledWith('org-1', 'sre#2', 'frontend');
    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith('Could not set the lane of sre#2', {
        description: 'It is in progress. Pause it or move it first.',
      }),
    );
    expect(toast.success).toHaveBeenCalledTimes(1);
    expect(toast.success).toHaveBeenCalledWith('Set the lane of sre#1 to Frontend.');
    // The items query refetches once the requests settle.
    await waitFor(() => expect(vi.mocked(listItems).mock.calls.length).toBeGreaterThan(listsBefore));
  });

  it('checks every part of an assembly from its group row', async () => {
    renderItems([assembly(), partOf(1, { title: 'Move the schema' }), partOf(2, { title: 'Backfill the ledger' })]);

    fireEvent.click(await screen.findByRole('checkbox', { name: 'Select the parts of Migrate billing' }));
    expect(within(await screen.findByRole('region', { name: 'Bulk actions' })).getByText('2 selected')).toBeTruthy();
  });
});
