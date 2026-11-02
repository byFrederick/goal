// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { type ReactNode, useState } from 'react';
import { toast } from 'sonner';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError, cancelItem } from '../../api/client';
import { keys } from '../../api/queries';
import type { Factory, Item, ItemEvent, OrgSummary } from '../../api/types';
import { TopBarSetContext, type TopBarState } from '../../app/top-bar';
import { itemKey } from '../../shared/factory';
import { renderFactoryPage, type Seed } from '../../test/route';
import { TooltipProvider } from '../../ui/tooltip';
import { ItemPage } from './item-page';

vi.mock(import('../../api/client'), async (original) => ({
  ...(await original()),
  cancelItem: vi.fn(async () => item({ state: 'cancelled' })),
}));
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

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
          {top?.back}
          {top?.crumb}
          {top?.actions}
        </section>
        {children}
      </TopBarSetContext.Provider>
    </TooltipProvider>
  );
}

function renderPage(
  it: Item,
  events: ItemEvent[] = [approval],
  grants: Factory = factory,
  search = '',
  loader?: () => Promise<unknown> | undefined,
) {
  return renderFactoryPage({
    ...(loader ? { loader } : {}),
    page: () => (
      <TopBarHarness>
        <ItemPage />
      </TopBarHarness>
    ),
    path: 'items/$ref',
    location: `items/147${search}`,
    org,
    factory: grants,
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

describe('the item layout', () => {
  function stubWidth(phone: boolean) {
    window.matchMedia = (query: string) =>
      ({
        matches: phone && query === '(max-width: 767px)',
        media: query,
        addEventListener: () => {},
        removeEventListener: () => {},
      }) as unknown as MediaQueryList;
  }

  it('shimmers the working line while an agent runs the item', async () => {
    stubWidth(false);
    const ev = (id: number, kind: string, data = {}): ItemEvent => ({
      id,
      item_id: 'item-147',
      at: '2026-10-25T10:00:00Z',
      kind,
      station: 'build',
      actor: 'builder',
      data,
      run_id: 'r1',
    });
    renderPage(item({ state: 'in_progress' }), [ev(1, 'claimed', { slot: 0, agent: 'builder' }), ev(2, 'run_started')]);

    const line = await screen.findByText(/working, 0 tool calls/);
    expect(line.classList).toContain('thinking');
  });

  it('puts the timeline before the Details card, with the tabs as tabs', async () => {
    stubWidth(false);
    renderPage(item());

    const panel = await screen.findByRole('tabpanel', { name: 'Timeline' });
    const details = screen.getByRole('heading', { name: 'Details' });
    expect(panel.compareDocumentPosition(details) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    const tabs = screen.getAllByRole('tab');
    expect(tabs.find((t) => t.getAttribute('aria-selected') === 'true')?.textContent).toBe('Timeline');
  });

  // jsdom has no container queries, so read the base (narrow) order-N class that decides the visual order.
  function narrowOrder(el: HTMLElement): number {
    let node: HTMLElement | null = el;
    while (node) {
      const m = /(?:^|\s)order-(\d+)(?:\s|$)/.exec(node.className);
      if (m) return Number(m[1]);
      node = node.parentElement;
    }
    return 0;
  }

  it.each([
    ['phone', true],
    ['tablet', false],
  ])('puts the tab row before every side card and after the question card at %s width', async (_name, phone) => {
    stubWidth(phone);
    renderPage(item({ question }));

    const tablist = await screen.findByRole('tablist', { name: 'Item views' });
    const tabs = narrowOrder(tablist);
    for (const name of ['Details', 'Cost', 'Overlaps', 'Rules']) {
      const heading = screen.queryByRole('heading', { name });
      if (heading) expect(narrowOrder(heading), name).toBeGreaterThan(tabs);
    }
    expect(screen.getByRole('heading', { name: 'Details' })).toBeTruthy();
    if (!phone) expect(screen.getByRole('heading', { name: 'Cost' })).toBeTruthy();
    expect(narrowOrder(screen.getByText(question.text))).toBeLessThan(tabs);
  });

  it('opens ?tab=teardown on the Teardown panel without the side cards', async () => {
    stubWidth(false);
    renderPage(item(), [approval], factory, '?tab=teardown');

    expect(await screen.findByRole('tabpanel', { name: 'Teardown' })).toBeTruthy();
    expect(screen.queryByRole('heading', { name: 'Details' })).toBeNull();
  });

  it('shows no side cards on Trace, and keeps them on Spec', async () => {
    stubWidth(false);
    const { unmount } = renderPage(item(), [approval], factory, '?tab=trace');
    expect(await screen.findByRole('tabpanel', { name: 'Trace' })).toBeTruthy();
    expect(screen.queryByRole('heading', { name: 'Details' })).toBeNull();
    unmount();

    renderPage(item(), [approval], factory, '?tab=spec');
    expect(await screen.findByRole('heading', { name: 'Details' })).toBeTruthy();
  });

  it('shows the loading skeleton as busy, with the same column order', async () => {
    const { ItemSkeleton } = await import('../../app/route-skeletons');
    const { container } = render(<ItemSkeleton />);
    expect(container.querySelector('[aria-busy="true"]')).not.toBeNull();
    expect(screen.getByText('Loading')).toBeTruthy();
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

  it('keeps Send back out of the phone card and in the More actions menu of the top bar', async () => {
    stubWidth(true);
    renderPage(item());

    const bar = await screen.findByLabelText('Top bar');
    expect(within(bar).queryByRole('button', { name: 'Send back to Spec' })).toBeNull();
    fireEvent.keyDown(await within(bar).findByRole('button', { name: 'More actions' }), { key: 'Enter' });
    expect(await screen.findByRole('menuitem', { name: 'Send back to Spec' })).toBeTruthy();
    expect(screen.getAllByRole('menuitem', { name: 'Send back to Spec' })).toHaveLength(1);
  });

  it('puts Back to Board, the ref and the More actions menu in the phone top bar, with no board link in the page', async () => {
    stubWidth(true);
    renderPage(item());

    const bar = await screen.findByLabelText('Top bar');
    const back = within(bar).getByRole('link', { name: 'Back to Board' });
    expect(back.getAttribute('href')).toBe('/o/acme/f/goal/board');
    expect(within(bar).getByText('#147')).toBeTruthy();
    expect(within(bar).getByRole('button', { name: 'More actions' })).toBeTruthy();
    expect(screen.queryByRole('link', { name: 'Goal board' })).toBeNull();
  });

  it('shows the summary line under the stepper, with the time at the station and in the line', async () => {
    stubWidth(true);
    renderPage(item());

    await screen.findByRole('heading', { level: 1 });
    expect(screen.getByText(/ in Build, .* in the line/)).toBeTruthy();
  });

  it('shows Latest in this run with All N events, which opens the Trace tab', async () => {
    stubWidth(true);
    const { router } = renderPage(item());

    const latest = await screen.findByRole('heading', { name: 'Latest in this run' });
    const section = latest.closest('section') as HTMLElement;
    fireEvent.click(within(section).getByRole('button', { name: /^All \d+ events$/ }));
    expect(await screen.findByRole('tabpanel', { name: 'Trace' })).toBeTruthy();
    expect(router.state.location.search).toEqual({ tab: 'trace' });
  });

  it('puts the tab row before the Latest card', async () => {
    stubWidth(true);
    renderPage(item());

    const latest = await screen.findByRole('heading', { name: 'Latest in this run' });
    const tabs = screen.getByRole('tablist', { name: 'Item views' });
    expect(tabs.compareDocumentPosition(latest) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('shows one Details card on a phone and no separate Cost card', async () => {
    stubWidth(true);
    renderPage(item());

    await screen.findByRole('heading', { level: 1 });
    expect(screen.getByRole('heading', { name: 'Details' })).toBeTruthy();
    expect(screen.queryByRole('heading', { name: 'Cost' })).toBeNull();
    expect(screen.getByText('Cost')).toBeTruthy();
    expect(screen.queryByText('Station')).toBeNull();
  });

  it('shows the phone note field as a labelled pill with a round Send note button', async () => {
    stubWidth(true);
    renderPage(item());

    const field = await screen.findAllByRole('textbox', { name: 'Note for the agent' });
    expect(field.length).toBeGreaterThan(0);
    expect(screen.getAllByRole('button', { name: 'Send note' }).length).toBeGreaterThan(0);
  });
});

describe('Cancel item on an item page', () => {
  const cancelled: Item = { ...item(), state: 'cancelled' };
  const done: Item = { ...item(), state: 'done' };
  const noDecide: Factory = { ...factory, can: ['item:operate'] };

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

  /** Opens the dialog from the desktop top bar's button. */
  async function openDialog() {
    const bar = await screen.findByLabelText('Top bar');
    fireEvent.click(within(bar).getByRole('button', { name: 'Cancel item' }));
    return screen.findByRole('dialog', { name: 'Cancel goal#147?' });
  }

  it('shows Cancel item in the top bar for an open item the caller may decide', async () => {
    stubWidth(false);
    renderPage(item());

    const bar = await screen.findByLabelText('Top bar');
    expect(within(bar).getByRole('button', { name: 'Cancel item' })).toBeTruthy();
  });

  it('hides Cancel item on a done item, a cancelled item and without item:decide', async () => {
    stubWidth(false);
    renderPage(done);
    await screen.findByRole('heading', { level: 1 });
    expect(screen.queryByRole('button', { name: 'Cancel item' })).toBeNull();
    cleanup();

    renderPage(cancelled);
    await screen.findByRole('heading', { level: 1 });
    expect(screen.queryByRole('button', { name: 'Cancel item' })).toBeNull();
    cleanup();

    renderPage(item(), [approval], noDecide);
    await screen.findByRole('heading', { level: 1 });
    expect(screen.queryByRole('button', { name: 'Cancel item' })).toBeNull();
  });

  it('puts Cancel item in the More actions menu on a phone, not as a button', async () => {
    stubWidth(true);
    renderPage(item());

    const bar = await screen.findByLabelText('Top bar');
    expect(within(bar).queryByRole('button', { name: 'Cancel item' })).toBeNull();
    fireEvent.keyDown(await within(bar).findByRole('button', { name: 'More actions' }), { key: 'Enter' });
    expect(await screen.findByRole('menuitem', { name: 'Cancel item' })).toBeTruthy();
  });

  it('opens the confirm dialog with its copy and cancels with the typed reason', async () => {
    stubWidth(false);
    renderPage(item());

    const dialog = await openDialog();
    expect(
      within(dialog).getByText(
        "The factory stops working on it and its pull request stays open. You can't reopen a cancelled item.",
      ),
    ).toBeTruthy();
    fireEvent.change(within(dialog).getByRole('textbox', { name: 'Reason' }), {
      target: { value: 'Duplicate of #12' },
    });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel item' }));

    await waitFor(() => expect(cancelItem).toHaveBeenCalledWith('org-1', key, 'Duplicate of #12'));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(toast.success).toHaveBeenCalledWith('Cancelled goal#147.');
  });

  it('cancels with no reason when the field is empty', async () => {
    stubWidth(false);
    renderPage(item());

    const dialog = await openDialog();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel item' }));

    await waitFor(() => expect(cancelItem).toHaveBeenCalledWith('org-1', key, undefined));
  });

  it('Escape closes the dialog and cancels nothing', async () => {
    stubWidth(false);
    renderPage(item());

    const dialog = await openDialog();
    fireEvent.keyDown(dialog, { key: 'Escape' });

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(cancelItem).not.toHaveBeenCalled();
  });

  it('shows a 409 refusal inline in the dialog, with no error toast, and clears it on reopen', async () => {
    stubWidth(false);
    vi.mocked(cancelItem).mockRejectedValueOnce(
      new ApiError({ title: 'Conflict', status: 409, code: 'item-closed', detail: 'goal#147 is done' }),
    );
    renderPage(item());

    const dialog = await openDialog();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel item' }));

    expect((await within(dialog).findByRole('alert')).textContent).toBe('goal#147 is done');
    expect(toast.error).not.toHaveBeenCalled();

    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    const reopened = await openDialog();
    expect(within(reopened).queryByRole('alert')).toBeNull();
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

/** The agent's open question, which the item carries (Item.question). */
const question = {
  text: 'Should the sidebar show the whole organization?',
  choices: ['The whole organization', 'Only this factory'],
  asked_at: new Date(Date.now() - 12 * 60_000).toISOString(),
  agent: 'frontend-engineer',
  run_id: 'run-147',
};

function renderTab(location: string, it: Item, events: ItemEvent[] = [approval]) {
  const seed: Seed[] = [
    [keys.item(org.id, key), it],
    [keys.events(org.id, key), events],
    [keys.itemRuns(org.id, key), { runs: [] }],
  ];
  return renderFactoryPage({
    page: () => (
      <TooltipProvider>
        <ItemPage />
      </TooltipProvider>
    ),
    path: 'items/$ref',
    location,
    org,
    factory,
    seed,
  });
}

const runEvent = (id: number, kind: string, data: Record<string, unknown> = {}, at = '2026-10-25T09:00:00Z') =>
  ({
    id,
    item_id: 'item-147',
    run_id: 'run-1',
    at,
    kind,
    station: 'build',
    actor: 'builder',
    data,
  }) as ItemEvent;

describe('the Trace tab', () => {
  it('lists the steps of a run, and its Full trace button opens the tab', async () => {
    const events = [
      approval,
      runEvent(2, 'run_started'),
      runEvent(3, 'tool_call', { id: 't1', tool: 'Read', input: '{"file_path":"src/app.ts"}' }, '2026-10-25T09:00:10Z'),
      runEvent(4, 'tool_result', { id: 't1', is_error: false, summary: 'Read 40 lines' }, '2026-10-25T09:00:11Z'),
    ];
    const { router } = renderTab('items/147', item(), events);

    fireEvent.click(await screen.findByRole('button', { name: 'Full trace' }));

    const panel = await screen.findByRole('tabpanel', { name: 'Trace' });
    expect(router.state.location.search).toEqual({ tab: 'trace' });
    expect(within(panel).getByRole('list', { name: 'Legend' })).toBeTruthy();
    expect(within(panel).getByRole('button', { name: /Read\s*src\/app\.ts/ })).toBeTruthy();
    expect(screen.getByRole('tab', { name: 'Trace' }).getAttribute('aria-selected')).toBe('true');
  });

  it('says there are no tool calls for an item with no runs', async () => {
    renderTab('items/147?tab=trace', item(), []);

    const panel = await screen.findByRole('tabpanel', { name: 'Trace' });
    expect(await within(panel).findByText('No tool calls yet.')).toBeTruthy();
  });

  it('no longer holds the trace in the main column of the Timeline tab beyond its run cards', async () => {
    renderTab('items/147', item());
    expect(await screen.findByRole('tabpanel', { name: 'Timeline' })).toBeTruthy();
    expect(screen.queryByRole('list', { name: 'Legend' })).toBeNull();
  });
});

describe('the question, the tabs and the spec', () => {
  it('asks the item’s open question on the Timeline tab, with its quick answers', async () => {
    renderTab('items/147', item({ question }));

    expect(await screen.findByText('Should the sidebar show the whole organization?')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'The whole organization' })).toBeTruthy();
  });

  it('shows no question card when the item has no open question', async () => {
    renderTab('items/147', item({ question: null }));

    expect(
      await screen.findByRole('heading', { level: 1, name: "Show this week's spend in the sidebar" }),
    ).toBeTruthy();
    expect(screen.queryByText('Should the sidebar show the whole organization?')).toBeNull();
  });

  it('opens the answer field from Write an answer on the card, not the note field', async () => {
    renderTab('items/147', item({ question }));

    fireEvent.click(await screen.findByRole('button', { name: 'Write an answer' }));

    expect(screen.getByRole('textbox', { name: 'Your answer' })).toBeTruthy();
  });

  it('lists the spec acceptance criteria on the Spec tab', async () => {
    renderTab(
      'items/147?tab=spec',
      item({ outputs: { spec: { acceptance: ['The sidebar shows the week’s spend', 'It reads in dark mode'] } } }),
    );

    expect(await screen.findByText('The sidebar shows the week’s spend')).toBeTruthy();
    expect(screen.getByText('It reads in dark mode')).toBeTruthy();
  });
});

describe('Watch live', () => {
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

  const running = (): Item => {
    const base = item();
    return item({
      state: 'in_progress',
      flow: {
        ...(base.flow as NonNullable<Item['flow']>),
        current_run: {
          id: 'run-1',
          agent: 'builder',
          slot: 1,
          model: 'claude-sonnet-5-5',
          turn: 2,
          started_at: '2026-10-25T10:00:00Z',
          output_contract: null,
          checks: [],
        },
      },
    });
  };

  it('opens the Trace tab on a running run and scrolls to its newest step', async () => {
    stubWidth(false);
    const scrollIntoView = vi.fn();
    Element.prototype.scrollIntoView = scrollIntoView;
    renderPage(running());

    const bar = await screen.findByLabelText('Top bar');
    const watch = within(bar).getByRole('button', { name: 'Watch live' });
    expect(watch.getAttribute('aria-disabled')).toBeNull();
    fireEvent.click(watch);

    expect(await screen.findByRole('tabpanel', { name: 'Trace' })).toBeTruthy();
    await waitFor(() => expect(scrollIntoView).toHaveBeenCalled());
  });

  it.each([
    [false, 'smooth'],
    [true, 'auto'],
  ])('scrolls the newest step on the first press from Timeline (reduced motion: %s)', async (reduced, behavior) => {
    window.matchMedia = ((query: string) =>
      ({
        matches: reduced && query === '(prefers-reduced-motion: reduce)',
        media: query,
        addEventListener: () => {},
        removeEventListener: () => {},
        addListener: () => {},
        removeListener: () => {},
        dispatchEvent: () => false,
      }) as unknown as MediaQueryList) as typeof window.matchMedia;
    const scrollIntoView = vi.fn();
    Element.prototype.scrollIntoView = scrollIntoView;
    // The router commits the new tab a render after the press, as it does in a browser.
    let release: () => void = () => {};
    let held = false;
    const loader = () => {
      if (!held) return undefined;
      return new Promise<void>((resolve) => {
        release = resolve;
      });
    };
    renderPage(running(), [approval], factory, '', loader);

    const bar = await screen.findByLabelText('Top bar');
    held = true;
    fireEvent.click(within(bar).getByRole('button', { name: 'Watch live' }));
    // Let the page commit the press while the tab is still Timeline.
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByRole('tabpanel', { name: 'Trace' })).toBeNull();
    release();

    await screen.findByRole('tabpanel', { name: 'Trace' });
    // The tab row also scrolls its selected tab into view; only the anchor past the newest step counts.
    const anchorCalls = () =>
      scrollIntoView.mock.calls.filter((_, i) =>
        (scrollIntoView.mock.contexts[i] as HTMLElement).className.includes('h-px'),
      );
    await waitFor(() => expect(anchorCalls()).toHaveLength(1));
    expect(anchorCalls()[0]).toEqual([{ block: 'end', behavior }]);
  });

  it('opens Trace without letting the router reset the scroll, which would undo the scroll to the newest step', async () => {
    stubWidth(false);
    const { router } = renderPage(running());
    const navigate = vi.spyOn(router, 'navigate');

    const bar = await screen.findByLabelText('Top bar');
    fireEvent.click(within(bar).getByRole('button', { name: 'Watch live' }));

    await screen.findByRole('tabpanel', { name: 'Trace' });
    expect(navigate).toHaveBeenCalledTimes(1);
    expect(navigate.mock.calls[0]?.[0]).toMatchObject({ search: { tab: 'trace' }, resetScroll: false });
  });

  it('is disabled with its reason when no run is working, and does not open the Trace tab', async () => {
    stubWidth(false);
    renderPage(item({ state: 'in_progress' }));

    const bar = await screen.findByLabelText('Top bar');
    const watch = within(bar).getByRole('button', { name: 'Watch live' });
    expect(watch.getAttribute('aria-disabled')).toBe('true');
    fireEvent.click(watch);
    fireEvent.keyDown(watch, { key: 'Enter' });
    fireEvent.keyDown(watch, { key: ' ' });
    expect(screen.queryByRole('tabpanel', { name: 'Trace' })).toBeNull();
    watch.focus();
    expect((await screen.findAllByText('No run is working on this item')).length).toBeGreaterThan(0);
  });

  it('is disabled when the item is not in progress, even with a run recorded', async () => {
    stubWidth(false);
    renderPage({ ...running(), state: 'waiting_human' });

    const bar = await screen.findByLabelText('Top bar');
    expect(within(bar).getByRole('button', { name: 'Watch live' }).getAttribute('aria-disabled')).toBe('true');
  });

  it('sits centred in its own card on a phone with a 44 px tap target', async () => {
    stubWidth(true);
    renderPage(running());

    const watch = await screen.findByRole('button', { name: 'Watch live' });
    expect(watch.className).toContain('max-md:min-h-11');
    expect(watch.parentElement?.className).toContain('justify-center');
    expect(within(screen.getByLabelText('Top bar')).queryByRole('button', { name: 'Watch live' })).toBeNull();
  });
});
