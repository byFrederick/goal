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
  listItems: vi.fn(async () => ({ items: shown.items, next_after: null })),
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

function renderItems(items: ItemCard[], entries: InboxEntry[] = []) {
  shown.items = items;
  return renderFactoryPage({
    page: ItemsPage,
    path: 'items/',
    org,
    factory,
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
