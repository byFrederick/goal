// @vitest-environment jsdom
import { cleanup, fireEvent, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { inboxQuery, itemsQuery } from '../../api/queries';
import type { Factory, InboxEntry, ItemCard, OrgSummary } from '../../api/types';
import { renderFactoryPage } from '../../test/route';
import { ItemsPage } from './items-page';

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
  id: 'item-3',
  ref: 'sre#3',
  number: 3,
  title: 'Ship the retry fix',
  type: 'task',
  station: 'merge',
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

const ship: InboxEntry = {
  id: 'decision:item-3',
  kind: 'decision',
  factory: { id: factory.id, name: factory.name, title: factory.title ?? null },
  since: '2026-10-25T09:00:00Z',
  station: 'merge',
  action: 'merge',
  item: item({}),
  approval: null,
  blocked: null,
  andon: null,
  rework: null,
};

function renderItems(entries: InboxEntry[] = []) {
  return renderFactoryPage({
    page: ItemsPage,
    path: 'items/',
    org,
    factory,
    seed: [
      [inboxQuery(org.id, factory.id).queryKey, { entries, truncated: false }],
      [itemsQuery(org.id, factory.id, { limit: 200 }).queryKey, { items: [item({})] }],
    ],
  });
}

describe('ItemsPage', () => {
  it('has no Needs you section on the phone', async () => {
    renderItems([ship]);

    expect(await screen.findByRole('heading', { level: 1, name: 'SRE items' })).toBeTruthy();
    expect(screen.queryByRole('region', { name: 'Needs you' })).toBeNull();
    expect(screen.queryByRole('heading', { name: 'Needs you' })).toBeNull();
  });

  it('the header Inbox button opens the inbox drawer, with its count', async () => {
    renderItems([ship]);

    const button = await screen.findByRole('button', { name: /^Inbox/ });
    expect(button.textContent).toContain('1');
    fireEvent.click(button);
    const drawer = await screen.findByRole('dialog', { name: 'Inbox' });
    expect(within(drawer).getByText('Ready to merge')).toBeTruthy();
  });
});
