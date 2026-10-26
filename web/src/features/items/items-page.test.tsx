// @vitest-environment jsdom
import { cleanup, fireEvent, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { boardQuery, inboxQuery } from '../../api/queries';
import type { Board, Factory, InboxEntry, ItemCard, OrgSummary } from '../../api/types';
import { renderFactoryPage } from '../../test/route';
import { ItemsPage } from './items-page';

// The list reads the items; the Inbox button and drawer read the inbox, which the tests seed.
const shown = vi.hoisted(() => ({ items: [] as ItemCard[] }));
vi.mock(import('../../api/client'), async (original) => ({
  ...(await original()),
  // Like the server: a state filter keeps the items in that state.
  listItems: vi.fn(async (_org: string, _factory: string, filter: { state?: string | undefined }) => ({
    items: filter.state ? shown.items.filter((it) => it.state === filter.state) : shown.items,
    next_after: null,
  })),
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

/** The status icons in a row: a StatusIcon is a shrink-0 svg; the row's chevron is not. */
function statusIcons(row: HTMLElement): string[] {
  return [...row.querySelectorAll('svg.shrink-0')].map((svg) => svg.getAttribute('class') ?? '');
}

/** The row (the list's link) that holds a title. */
async function rowOf(title: string): Promise<HTMLElement> {
  return (await screen.findByText(title)).closest('a') as HTMLElement;
}

function renderItems(items: ItemCard[], entries: InboxEntry[] = [], search: Record<string, string> = {}) {
  shown.items = items;
  return renderFactoryPage({
    page: ItemsPage,
    path: 'items/',
    org,
    factory,
    search,
    seed: [
      [boardQuery(org.id, factory.id).queryKey, board],
      [inboxQuery(org.id, factory.id).queryKey, { entries, truncated: false }],
    ],
  });
}

const ship: InboxEntry = {
  id: 'decision:item-3',
  kind: 'decision',
  factory: { id: factory.id, name: factory.name, title: factory.title ?? null },
  since: '2026-10-25T09:00:00Z',
  station: 'merge',
  action: 'merge',
  item: item({ id: 'item-3', ref: 'sre#3', number: 3, title: 'Ship the retry fix', station: 'merge' }),
  approval: null,
  blocked: null,
  andon: null,
  rework: null,
};

describe('ItemsPage inbox', () => {
  it('has no Needs you section on the phone', async () => {
    renderItems([item({ title: 'Ship the retry fix' })], [ship]);

    expect(await screen.findByRole('heading', { level: 1, name: 'SRE items' })).toBeTruthy();
    expect(screen.queryByRole('region', { name: 'Needs you' })).toBeNull();
    expect(screen.queryByRole('heading', { name: 'Needs you' })).toBeNull();
  });

  it('the header Inbox button opens the inbox drawer, with its count', async () => {
    renderItems([item({ title: 'Ship the retry fix' })], [ship]);

    const button = await screen.findByRole('button', { name: /^Inbox/ });
    expect(button.textContent).toContain('1');
    fireEvent.click(button);
    const drawer = await screen.findByRole('dialog', { name: 'Inbox' });
    expect(within(drawer).getByText('Ready to merge')).toBeTruthy();
  });
});

describe('Items list status icons', () => {
  it('shows the working icon for an item in progress', async () => {
    renderItems([item({ title: 'Checkout API latency', state: 'in_progress', agent: 'infra-engineer' })]);

    const row = await rowOf('Checkout API latency');
    expect(screen.getAllByText('Infra engineer working').length).toBeGreaterThan(0);
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
    expect(icons.every((c) => c.includes('text-ok-dot'))).toBe(true);
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
    parts: { total: 3, merged: 1, in_progress: 1, waiting: 1, stations: ['build', 'review'] },
    ...over,
  });

const linkTitles = () => [...document.querySelectorAll('a')].map((a) => a.textContent ?? '');

describe('Items list assemblies', () => {
  it('shows an assembly as one row with its parts beneath it, in plan order', async () => {
    renderItems([
      assembly(),
      partOf(2, { title: 'Backfill the ledger', state: 'queued', station: 'build' }),
      partOf(1, { title: 'Move the schema', state: 'done', station: 'merge' }),
      item({ id: 'plain', title: 'Triage the export', type: 'incident', state: 'queued' }),
    ]);

    const toggle = await screen.findByRole('button', { name: 'Parts of Migrate billing' });
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByText('1 of 3 parts merged')).toBeTruthy();
    expect(screen.getByText('Build, Review')).toBeTruthy();
    expect(screen.getAllByTitle('Migrate billing').length).toBeGreaterThan(0);
    expect(screen.getAllByText('Assembly').length).toBeGreaterThan(0);

    const titles = linkTitles();
    expect(titles.findIndex((t) => t.includes('Move the schema'))).toBeLessThan(
      titles.findIndex((t) => t.includes('Backfill the ledger')),
    );
    // A part never also shows as a top-level row: its title is in one link only.
    expect(screen.getAllByText('Move the schema')).toHaveLength(1);
    // Only the parts are indented, and a plain item has no type chip.
    expect(document.querySelectorAll('ul ul')).toHaveLength(1);
    expect(screen.queryByText('Incident')).toBeNull();
    expect(screen.getByText('Triage the export')).toBeTruthy();
  });

  it('shows a part with its station and its status in the shared words and icon', async () => {
    renderItems([assembly(), partOf(1, { title: 'Move the schema', state: 'done', station: 'merge' })]);

    const row = (await screen.findByText('Move the schema')).closest('a') as HTMLElement;
    expect(within(row).getAllByText('Merge').length).toBeGreaterThan(0);
    expect(within(row).getAllByText('Done').length).toBeGreaterThan(0);
    const icons = [...row.querySelectorAll('svg.shrink-0')].map((svg) => svg.getAttribute('class') ?? '');
    expect(icons.length).toBeGreaterThan(0);
    expect(icons.every((c) => c.includes('text-ok-dot'))).toBe(true);
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

  it('lists every item flat, with no assembly row and no indented part, when Show is Every item flat', async () => {
    renderItems([assembly(), partOf(1, { title: 'Move the schema', state: 'done' })]);

    fireEvent.click(await screen.findByLabelText('Every item flat'));
    expect(
      await screen.findByText(
        'Every item is listed on its own, assemblies and parts included. Filters apply to every item.',
      ),
    ).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Parts of Migrate billing' })).toBeNull();
    expect(document.querySelectorAll('ul ul')).toHaveLength(0);
    expect(document.querySelectorAll('ul.rounded-card > li')).toHaveLength(2);
    expect(screen.getByText('Move the schema')).toBeTruthy();
  });

  it('keeps the Show choice across a filter change', async () => {
    renderItems([assembly(), partOf(1, { title: 'Move the schema', state: 'done' })], [], { show: 'flat' });

    expect((await screen.findByLabelText('Every item flat')) as HTMLInputElement).toHaveProperty('checked', true);
    fireEvent.click(screen.getByRole('link', { name: 'Done' }));
    expect(await screen.findByLabelText('Every item flat')).toHaveProperty('checked', true);
  });

  it('applies a filter to parts, under their assembly, with the assembly progress from the assemblies', async () => {
    renderItems(
      [
        assembly(),
        assembly({
          id: 'asm-2',
          ref: 'sre#20',
          title: 'Other assembly',
          parts: { total: 2, merged: 0, in_progress: 0, waiting: 2, stations: ['build'] },
        }),
        partOf(1, { title: 'Move the schema', state: 'in_progress', agent: 'infra-engineer' }),
        partOf(2, { title: 'Backfill the ledger', state: 'queued' }),
        {
          ...partOf(3, { title: 'Other part', state: 'queued' }),
          assembly: { id: 'asm-2', ref: 'sre#20', title: 'Other assembly', part: 1, parts: 2 },
        },
        item({ id: 'plain', title: 'Checkout API latency', state: 'in_progress' }),
      ],
      [],
      { state: 'in_progress' },
    );

    expect(await screen.findByText('Move the schema')).toBeTruthy();
    // The assemblies come from their own query, which the loader fills in production.
    expect(await screen.findByText('1 of 3 parts merged')).toBeTruthy();
    expect(screen.getByText('Checkout API latency')).toBeTruthy();
    expect(screen.queryByText('Backfill the ledger')).toBeNull();
    expect(screen.queryByText('Other part')).toBeNull();
    expect(screen.queryByText('Other assembly')).toBeNull();
  });

  it('builds an assembly row from a part when the assembly is not on the page, without a progress bar', async () => {
    renderItems([partOf(2, { title: 'Backfill the ledger', state: 'queued' })]);

    expect(await screen.findByRole('button', { name: 'Parts of Migrate billing' })).toBeTruthy();
    expect(screen.getByText('3 parts')).toBeTruthy();
    expect(screen.queryByText(/parts merged/)).toBeNull();
    expect(screen.getByText('Backfill the ledger')).toBeTruthy();
  });

  it('footnotes how the list groups and filters', async () => {
    renderItems([assembly(), partOf(1, { title: 'Move the schema', state: 'done' })]);

    expect(await screen.findByText('Parts are grouped under their assembly. Filters apply to parts.')).toBeTruthy();
  });
});
