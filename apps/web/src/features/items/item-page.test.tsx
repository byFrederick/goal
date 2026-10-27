// @vitest-environment jsdom
import { cleanup, fireEvent, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { inboxQuery, itemQuery, keys, runsQuery } from '../../api/queries';
import type { Factory, InboxEntry, Item, OrgSummary } from '../../api/types';
import { renderFactoryPage, type Seed } from '../../test/route';
import { TooltipProvider } from '../../ui/tooltip';
import { ItemPage } from './item-page';

afterEach(cleanup);

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: null };
const factory: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'software',
  title: 'Software',
  work_types: ['feature'],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'approver',
  can: ['item:operate', 'item:decide'],
};

const key = 'software#147';

const item = {
  id: 'item-147',
  ref: key,
  factory_id: 'factory-1',
  number: 147,
  type: 'feature',
  title: "Show this week's spend",
  body: 'The sidebar shows the spend of the week.',
  station: 'build',
  state: 'waiting_human',
  class_of_service: 'standard',
  priority: 0,
  rework_count: 0,
  cost_usd: 0.5,
  outputs: {
    spec: {
      acceptance: ['The sidebar shows the week’s spend', 'It reads in dark mode'],
    },
  },
  created_at: '2026-10-20T00:00:00Z',
  updated_at: '2026-10-25T00:00:00Z',
  touches: { count: 0, planned: 0, unplanned: 0, list: [] },
  flow: {
    spans: [],
    target_seconds: null,
    rework_target: null,
    budget: { per_item_usd: 4, max_turns_per_task: null },
    current_run: null,
  },
} as unknown as Item;

/** The open decision on the item: the question card's source. */
const question = {
  id: 'decision:item-147',
  kind: 'decision',
  factory: { id: 'factory-1', name: 'software', title: null },
  since: new Date(Date.now() - 12 * 60_000).toISOString(),
  item: { id: 'item-147' },
  station: 'build',
  action: null,
  approval: { summary: 'Should the sidebar show the whole organization?', facts: [], pull_request: null, spend_usd: 2 },
  blocked: null,
  andon: null,
  rework: null,
} as unknown as InboxEntry;

function renderItem(tab: 'timeline' | 'spec') {
  const seed: Seed[] = [
    [itemQuery(org.id, key).queryKey, item],
    [keys.events(org.id, key), []],
    [runsQuery(org.id, key).queryKey, { runs: [] }],
    [inboxQuery(org.id, factory.id).queryKey, { entries: [question] }],
  ];
  return renderFactoryPage({
    page: () => (
      <TooltipProvider>
        <ItemPage />
      </TooltipProvider>
    ),
    path: 'items/$ref',
    location: `items/147${tab === 'timeline' ? '' : `?tab=${tab}`}`,
    org,
    factory,
    seed,
  });
}

describe('ItemPage', () => {
  it('focuses the note field from the Spec tab, where the composer is not in the tab', async () => {
    renderItem('spec');

    fireEvent.click(await screen.findByRole('button', { name: 'Write an answer' }));

    // Both composers (the wide one and the phone one) carry the same field name; the focus lands in one of them.
    const fields = screen.getAllByRole('textbox', { name: 'Note for the agent' });
    expect(fields).toContain(document.activeElement);
  });

  it('lists the spec acceptance criteria on the Spec tab', async () => {
    renderItem('spec');

    expect(await screen.findByText('The sidebar shows the week’s spend')).toBeTruthy();
    expect(screen.getByText('It reads in dark mode')).toBeTruthy();
  });

  it('asks the question from the open decision on the Timeline tab', async () => {
    renderItem('timeline');

    expect(await screen.findByText('Should the sidebar show the whole organization?')).toBeTruthy();
  });
});
