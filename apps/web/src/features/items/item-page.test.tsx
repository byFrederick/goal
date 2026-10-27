// @vitest-environment jsdom
import { cleanup, fireEvent, screen, within } from '@testing-library/react';
import { type ReactNode, useState } from 'react';
import { afterEach, describe, expect, it } from 'vitest';
import { keys } from '../../api/queries';
import type { Factory, Item, ItemEvent, OrgSummary } from '../../api/types';
import { TopBarSetContext, type TopBarState } from '../../app/top-bar';
import { itemKey } from '../../shared/factory';
import { renderFactoryPage } from '../../test/route';
import { TooltipProvider } from '../../ui/tooltip';
import { ItemPage } from './item-page';

afterEach(cleanup);

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: null };
const factory: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'goal',
  title: 'Goal',
  work_types: ['feature'],
  stations: [
    { id: 'intake', kind: 'queue' },
    { id: 'spec', kind: 'agent', description: 'Writes the spec' },
    { id: 'build', kind: 'agent' },
    { id: 'review', kind: 'agent', description: 'Then merge' },
    { id: 'done', kind: 'done' },
  ],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'approver',
  can: ['item:operate', 'item:decide'],
};

const key = itemKey(factory.name, '147');

/** The item as the picture has it: at Build, waiting on a person, part 4 of 4 of an assembly. */
function item(overrides: Partial<Item> = {}): Item {
  return {
    id: 'item-147',
    ref: 'goal#147',
    factory_id: 'factory-1',
    number: 147,
    type: 'feature',
    lane: 'frontend',
    title: "Show this week's spend in the sidebar",
    body: '',
    station: 'build',
    state: 'waiting_human',
    class_of_service: 'standard',
    priority: 0,
    rework_count: 0,
    cost_usd: 3,
    outputs: {},
    created_at: '2026-10-25T08:00:00Z',
    updated_at: '2026-10-25T10:12:00Z',
    flow: {
      spans: [
        {
          station: 'intake',
          state: 'done',
          rework_round: 0,
          rework_by: null,
          entered_at: '2026-10-25T08:00:00Z',
          left_at: '2026-10-25T08:12:00Z',
        },
        {
          station: 'spec',
          state: 'done',
          rework_round: 0,
          rework_by: null,
          entered_at: '2026-10-25T08:12:00Z',
          left_at: '2026-10-25T08:50:00Z',
        },
        {
          station: 'build',
          state: 'waiting_human',
          rework_round: 0,
          rework_by: null,
          entered_at: '2026-10-25T08:50:00Z',
          left_at: null,
        },
      ],
      target_seconds: 48 * 3600,
      rework_target: 'spec',
      budget: { per_item_usd: 12, max_turns_per_task: null },
      current_run: null,
    },
    ...overrides,
  };
}

const approval: ItemEvent = {
  id: 1,
  item_id: 'item-147',
  at: '2026-10-25T08:49:00Z',
  kind: 'approved',
  station: 'spec',
  actor: 'Freddy Cruz',
  data: {},
};

/** The top bar's crumb and actions, shown where the page sets them (its TopBarSlot). */
function TopBarHarness({ children }: { children: ReactNode }) {
  const [top, setTop] = useState<TopBarState | null>(null);
  return (
    <TooltipProvider>
      <TopBarSetContext.Provider value={setTop}>
        <section aria-label="Top bar">
          {top?.crumb}
          {top?.actions}
        </section>
        {children}
      </TopBarSetContext.Provider>
    </TooltipProvider>
  );
}

function renderPage(it: Item, events: ItemEvent[] = [approval]) {
  return renderFactoryPage({
    page: () => (
      <TopBarHarness>
        <ItemPage />
      </TopBarHarness>
    ),
    path: 'items/$ref',
    location: 'items/147',
    org,
    factory,
    seed: [
      [keys.item(org.id, key), it],
      [keys.events(org.id, key), events],
      [keys.itemRuns(org.id, key), { runs: [] }],
    ],
  });
}

describe('the item page header', () => {
  it('shows the ref, the work type, the lane and the assembly chip, then the title', async () => {
    renderPage(item({ assembly: { id: 'item-144', ref: 'goal#144', title: 'Usage and budgets', part: 4, parts: 4 } }));

    expect(
      await screen.findByRole('heading', { level: 1, name: "Show this week's spend in the sidebar" }),
    ).toBeTruthy();
    expect(screen.getAllByText('#147')).toHaveLength(2);
    expect(screen.getByText('feature')).toBeTruthy();
    expect(screen.getByText('frontend')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Usage and budgets, part 4 of 4' }).getAttribute('href')).toContain(
      '/items/144',
    );
  });

  it('shows no assembly chip for an item that is not a part', async () => {
    renderPage(item());

    await screen.findByRole('heading', { level: 1 });
    expect(screen.queryByText(/part \d+ of \d+/)).toBeNull();
  });

  it('puts the ref in the top bar crumb and Watch live, Pause, Send back and More actions in its actions', async () => {
    renderPage(item());

    const bar = await screen.findByLabelText('Top bar');
    expect(within(bar).getByText('#147')).toBeTruthy();
    const watch = within(bar).getByRole('button', { name: 'Watch live' });
    expect(watch.getAttribute('aria-disabled')).toBe('true');
    expect(within(bar).getByRole('button', { name: 'Pause' })).toBeTruthy();
    expect(within(bar).getByRole('button', { name: 'Send back to Spec' })).toBeTruthy();
    expect(within(bar).getByRole('button', { name: 'More actions' })).toBeTruthy();
  });

  it('lists Move in the More actions menu', async () => {
    renderPage(item());

    const bar = await screen.findByLabelText('Top bar');
    const more = await within(bar).findByRole('button', { name: 'More actions' });
    expect(within(bar).queryByRole('button', { name: 'Move' })).toBeNull();
    fireEvent.keyDown(more, { key: 'Enter' });
    expect(await screen.findByRole('menuitem', { name: 'Move' })).toBeTruthy();
  });
});

describe('the item decisions at each width', () => {
  // The top bar holds the decisions from 768 px; a phone has them in a card under the title. Never both.
  function stubWidth(phone: boolean) {
    window.matchMedia = (query: string) =>
      ({
        matches: phone && query === '(max-width: 767px)',
        media: query,
        addEventListener: () => {},
        removeEventListener: () => {},
        addListener: () => {},
        removeListener: () => {},
        dispatchEvent: () => false,
      }) as unknown as MediaQueryList;
  }

  afterEach(() => {
    delete (window as { matchMedia?: unknown }).matchMedia;
  });

  it('shows Send back once at 768 px and wider, in the top bar', async () => {
    stubWidth(false);
    renderPage(item());

    await screen.findByRole('heading', { level: 1 });
    expect(screen.getAllByRole('button', { name: 'Send back to Spec' })).toHaveLength(1);
    expect(within(screen.getByLabelText('Top bar')).getByRole('button', { name: 'Send back to Spec' })).toBeTruthy();
  });

  it('shows Send back once on a phone, in its card under the title', async () => {
    stubWidth(true);
    renderPage(item());

    await screen.findByRole('heading', { level: 1 });
    expect(screen.getAllByRole('button', { name: 'Send back to Spec' })).toHaveLength(1);
    expect(within(screen.getByLabelText('Top bar')).queryByRole('button', { name: 'Send back to Spec' })).toBeNull();
  });
});

describe('the stepper', () => {
  it('lists every station, the done ones with their time and outcome', async () => {
    renderPage(item());

    const steps = await screen.findByRole('list', { name: 'Progress through the line' });
    const items = within(steps).getAllByRole('listitem');
    expect(items).toHaveLength(5);
    expect(items[0]?.textContent).toContain('Intake');
    expect(items[1]?.textContent).toContain('Spec');
    expect(items[1]?.textContent).toContain('accepted');
    expect(items[2]?.textContent).toContain('Build');
    expect(items[2]?.textContent).toContain('waiting on you');
    expect(items[2]?.getAttribute('aria-current')).toBe('step');
    expect(items[4]?.textContent).toContain('Target 48 h');
  });
});
