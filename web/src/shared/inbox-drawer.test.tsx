// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { approveItem, getInbox, mergeItem, retryItem } from '../api/client';
import type { Factory, InboxEntry, Item, ItemCard, OrgSummary } from '../api/types';
import { renderBoardPage } from '../test/route';
import { InboxButton } from './inbox-drawer';
import { reasons } from './permissions';

// The inbox and the actions are the API client's; the drawer's own behavior is what these tests see.
const state = vi.hoisted(() => ({ entries: [] as InboxEntry[] }));
/** What goald answers for the item after an action, cut to what the drawer reads. */
const answered = (over: Partial<Item>) => ({ state: 'queued', station: 'build', ...over }) as Item;
vi.mock(import('../api/client'), async (original) => ({
  ...(await original()),
  getInbox: vi.fn(async () => ({ entries: state.entries, truncated: false })),
  approveItem: vi.fn(async () => answered({})),
  retryItem: vi.fn(async () => answered({})),
  mergeItem: vi.fn(async () => answered({ state: 'done', station: 'merge' })),
}));

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 2, role: null };
const sre: Factory = {
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
const billing: Factory = { ...sre, id: 'factory-2', name: 'billing', title: 'Billing', role: 'viewer', can: [] };

const item = (over: Partial<ItemCard>): ItemCard => ({
  id: 'item-1',
  ref: 'sre#1',
  number: 1,
  title: 'Untitled',
  type: 'task',
  station: 'review',
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

const entry = (over: Partial<InboxEntry> & Pick<InboxEntry, 'id' | 'kind'>, f: Factory = sre): InboxEntry => ({
  factory: { id: f.id, name: f.name, title: f.title ?? null },
  since: '2026-10-25T08:00:00Z',
  item: null,
  station: 'review',
  action: null,
  approval: null,
  blocked: null,
  andon: null,
  rework: null,
  ...over,
});

const ship = entry({
  id: 'decision:item-3',
  kind: 'decision',
  action: 'merge',
  station: 'merge',
  since: '2026-10-25T09:00:00Z',
  item: item({ id: 'item-3', ref: 'sre#3', number: 3, title: 'Ship the retry fix', station: 'merge' }),
});
const shipOlder = entry({
  id: 'decision:item-4',
  kind: 'decision',
  action: 'merge',
  station: 'merge',
  since: '2026-10-25T07:00:00Z',
  item: item({ id: 'item-4', ref: 'sre#4', number: 4, title: 'Roll out the cache', station: 'merge' }),
});
const approve = entry({
  id: 'decision:item-5',
  kind: 'decision',
  since: '2026-10-25T06:00:00Z',
  item: item({ id: 'item-5', ref: 'sre#5', number: 5, title: 'Approve the schema change' }),
});
const retry = entry({
  id: 'blocked:item-6',
  kind: 'blocked',
  blocked: { reason: 'Tests failing', paused: false },
  item: item({ id: 'item-6', ref: 'sre#6', number: 6, title: 'Fix the flaky export' }),
});
const expedite = entry({
  id: 'expedite:item-7',
  kind: 'expedite',
  item: item({ id: 'item-7', ref: 'sre#7', number: 7, title: 'Checkout latency', class_of_service: 'expedite' }),
});

/** The Inbox button on a board header, with the inbox seeded from `entries`. */
function renderInbox(entries: InboxEntry[], factories: Factory[] = [sre, billing]) {
  state.entries = entries;
  return renderBoardPage({
    page: () => <InboxButton factory={sre} />,
    org,
    factory: sre,
    factories,
  });
}

async function openInbox() {
  fireEvent.click(await screen.findByRole('button', { name: /^Inbox/ }));
  return screen.findByRole('dialog', { name: 'Inbox' });
}

describe('InboxDrawer', () => {
  it('groups the rows Ready to merge, Needs a decision, Blocked, and leaves out expedite', async () => {
    renderInbox([approve, retry, ship, expedite]);
    const drawer = await openInbox();

    const headings = within(drawer)
      .getAllByRole('heading', { level: 3 })
      .map((h) => h.textContent);
    expect(headings).toEqual(['Ready to merge', 'Needs a decision', 'Blocked']);
    expect(within(drawer).queryByText('Checkout latency')).toBeNull();
    expect(within(drawer).getByRole('link', { name: /Ship the retry fix/ })).toBeTruthy();
  });

  it('shows the count on the Inbox button and "Nothing needs you" with no entries', async () => {
    renderInbox([ship, approve]);
    await waitFor(() => expect(screen.getByRole('button', { name: /^Inbox/ }).textContent).toContain('2'));
    cleanup();

    renderInbox([]);
    await openInbox();
    expect(await screen.findByText('Nothing needs you')).toBeTruthy();
  });

  it('Approve calls approveItem, Retry calls retryItem', async () => {
    renderInbox([approve, retry]);
    const drawer = await openInbox();

    fireEvent.click(await within(drawer).findByRole('button', { name: 'Approve sre#5' }));
    await waitFor(() => expect(approveItem).toHaveBeenCalledWith('org-1', 'sre#5'));

    fireEvent.click(within(drawer).getByRole('button', { name: 'Retry sre#6' }));
    await waitFor(() => expect(retryItem).toHaveBeenCalledWith('org-1', 'sre#6'));
  });

  it('Merge asks first, then calls mergeItem', async () => {
    renderInbox([ship]);
    const drawer = await openInbox();

    fireEvent.click(within(drawer).getByRole('button', { name: 'Merge sre#3' }));
    expect(mergeItem).not.toHaveBeenCalled();
    const confirm = await screen.findByRole('dialog', { name: /^Merge / });
    fireEvent.click(within(confirm).getByRole('button', { name: 'Squash and merge' }));

    await waitFor(() => expect(mergeItem).toHaveBeenCalledWith('org-1', 'sre#3'));
  });

  it('disables the action on an entry whose factory the person may not decide in, and says who decides', async () => {
    renderInbox(
      [entry({ id: 'decision:item-8', kind: 'decision', item: item({ ref: 'billing#8', title: 'Pay' }) }, billing)],
      [sre, billing],
    );
    const drawer = await openInbox();

    const approveButton = await within(drawer).findByRole('button', { name: 'Approve billing#8' });
    expect((approveButton as HTMLButtonElement).disabled).toBe(true);
    expect(within(drawer).getByText(reasons.decide('billing'))).toBeTruthy();
  });

  it('Merge all appears with more than one ready row and merges each after the confirmation', async () => {
    renderInbox([ship, shipOlder]);
    const drawer = await openInbox();

    fireEvent.click(within(drawer).getByRole('button', { name: 'Merge all' }));
    const confirm = await screen.findByRole('dialog', { name: 'Merge 2 items?' });
    fireEvent.click(within(confirm).getByRole('button', { name: 'Merge 2 items' }));

    await waitFor(() => expect(mergeItem).toHaveBeenCalledTimes(2));
    expect(mergeItem).toHaveBeenNthCalledWith(1, 'org-1', 'sre#4');
    expect(mergeItem).toHaveBeenNthCalledWith(2, 'org-1', 'sre#3');
  });

  it('Merge all is left out with a single ready row', async () => {
    renderInbox([ship]);
    const drawer = await openInbox();
    await within(drawer).findByRole('button', { name: 'Merge sre#3' });
    expect(within(drawer).queryByRole('button', { name: 'Merge all' })).toBeNull();
  });

  it('a handled row leaves the drawer after the inbox refetches', async () => {
    renderInbox([approve, retry]);
    const drawer = await openInbox();
    fireEvent.click(await within(drawer).findByRole('button', { name: 'Approve sre#5' }));

    // goald has now handled it: the refetch no longer lists the entry.
    state.entries = [retry];
    await waitFor(() => expect(within(drawer).queryByRole('button', { name: 'Approve sre#5' })).toBeNull());
    expect(within(drawer).getByRole('button', { name: 'Retry sre#6' })).toBeTruthy();
  });

  it('switches between this factory and all factories', async () => {
    renderInbox([ship]);
    const drawer = await openInbox();
    await within(drawer).findByRole('button', { name: 'Merge sre#3' });
    expect(getInbox).toHaveBeenLastCalledWith('org-1', { factory: 'factory-1' }, expect.anything());

    fireEvent.click(within(drawer).getByRole('radio', { name: 'All factories' }));
    await waitFor(() => expect(getInbox).toHaveBeenLastCalledWith('org-1', { factory: undefined }, expect.anything()));
  });

  it('Escape closes the drawer and returns focus to the Inbox button', async () => {
    renderInbox([ship]);
    const trigger = await screen.findByRole('button', { name: /^Inbox/ });
    // A real click focuses the button; fireEvent.click does not.
    trigger.focus();
    fireEvent.click(trigger);
    const drawer = await screen.findByRole('dialog', { name: 'Inbox' });
    await within(drawer).findByRole('button', { name: 'Merge sre#3' });

    fireEvent.keyDown(drawer, { key: 'Escape' });
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Inbox' })).toBeNull());
    expect(document.activeElement).toBe(trigger);
  });

  it('keeps focus inside the drawer while it is open', async () => {
    renderInbox([ship]);
    const drawer = await openInbox();
    await within(drawer).findByRole('button', { name: 'Merge sre#3' });
    expect(drawer.contains(document.activeElement)).toBe(true);
  });
});
