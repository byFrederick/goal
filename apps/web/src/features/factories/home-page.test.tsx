// @vitest-environment jsdom

import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { approveItem, decideToolApproval } from '../../api/client';
import {
  attentionQuery,
  boardQuery,
  inboxQuery,
  meQuery,
  metricsQuery,
  orgQuery,
  overviewQuery,
} from '../../api/queries';
import type { Board, Factory, FactoryOverview, FactoryOverviewList, Inbox, Item, OrgSummary } from '../../api/types';
import { renderOrgPage, type Seed } from '../../test/route';
import { FactoriesPage, summary } from './factories-page';

// The organization's services, faked: the page's queries are seeded, and only the error, loading and
// decision tests reach these.
const fake = vi.hoisted(() => ({
  overview: null as null | (() => Promise<never>),
  board: null as null | ((org: string, factory: string) => Promise<never>),
}));

vi.mock(import('../../api/client'), async (original) => ({
  ...(await original()),
  getFactoryOverview: vi.fn(() => (fake.overview ? fake.overview() : Promise.reject(new Error('no overview')))),
  getBoard: vi.fn((org: string, factory: string) => (fake.board ? fake.board(org, factory) : Promise.reject())),
  decideToolApproval: vi.fn(async () => ({ id: 'id-software#14', state: 'queued' }) as unknown as Item),
  approveItem: vi.fn(
    async (_org: string, item: string) => ({ ref: item, state: 'done', station: 'done' }) as unknown as Item,
  ),
}));

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  fake.overview = null;
  fake.board = null;
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 3, role: 'owner' };

function factory(id: string, name: string, can: string[] = ['item:decide', 'item:create']): Factory {
  return {
    id,
    org_id: org.id,
    name,
    title: name,
    work_types: [],
    stations: [],
    current: null,
    draft: null,
    paused: false,
    created_at: '2026-09-01T00:00:00Z',
    role: 'editor',
    can,
  };
}

const software = factory('f-software', 'software');
const sre = factory('f-sre', 'sre');
const support = factory('f-support', 'support', ['item:create']);

/** One factory's week, with the fields the page reads. */
function week(f: Factory, extra: Record<string, unknown> = {}): FactoryOverview {
  return {
    factory: f,
    health: 'running',
    andon: null,
    throughput: { per_day: 7.6, previous_per_day: 7, days: [] },
    constraint: null,
    waiting: { on_you: 0, items_by_type: {}, tool_approvals: 0 },
    spend_usd: 96,
    budget_usd: 150,
    ...extra,
  } as unknown as FactoryOverview;
}

const andon = {
  id: 7,
  kind: 'manual',
  station: null,
  reason: 'Answers sent back 4 of the last 10 times',
  pulled_by: 'user:local',
  pulled_at: '2026-10-27T10:00:00Z',
  cleared_by: null,
  cleared_at: null,
  note: null,
  data: {},
} as const;

const sreWeek = week(sre, {
  health: 'stopped',
  andon,
  waiting: { on_you: 1, items_by_type: {}, tool_approvals: 0 },
  spend_usd: 61,
  budget_usd: 400,
});

const overview = {
  window: '7d',
  since: '2026-10-21T00:00:00Z',
  until: '2026-10-27T00:00:00Z',
  waiting_on_you: 5,
  spend_usd: 512,
  budget_usd: 1400,
  factories: [
    week(software, { waiting: { on_you: 3, items_by_type: {}, tool_approvals: 0 }, spend_usd: 412, budget_usd: 800 }),
    sreWeek,
    week(support, { waiting: { on_you: 1, items_by_type: {}, tool_approvals: 0 }, spend_usd: 39, budget_usd: 200 }),
  ],
} as unknown as FactoryOverviewList;

const today = {
  ...overview,
  window: '1d',
  spend_usd: 149,
  factories: [
    { ...overview.factories[0], spend_usd: 96, budget_usd: 150 },
    { ...overview.factories[1], spend_usd: 31, budget_usd: 80 },
    { ...overview.factories[2], spend_usd: 22, budget_usd: 60 },
  ],
} as unknown as FactoryOverviewList;

const attention = {
  waiting_on_you: 5,
  factories: [
    { factory_id: software.id, waiting_on_you: 3, blocked_items: 0, andon: null, connections_blocked: 0 },
    { factory_id: sre.id, waiting_on_you: 1, blocked_items: 0, andon, connections_blocked: 0 },
    { factory_id: support.id, waiting_on_you: 1, blocked_items: 0, andon: null, connections_blocked: 0 },
  ],
};

const item = (ref: string, title: string, state = 'approve') => ({
  id: `id-${ref}`,
  ref,
  number: Number(ref.split('#')[1]),
  title,
  type: 'feature',
  station: 'approve',
  state,
  pull_requests: [],
});

const inboxEntries = (entries: unknown[]): Inbox => ({ entries, truncated: false }) as unknown as Inbox;

const decision = {
  id: 'decision:software#12',
  kind: 'decision',
  factory: { id: software.id, name: 'software', title: 'Software' },
  since: '2026-10-27T09:00:00Z',
  item: item('software#12', 'Restyle the Board'),
  station: 'approve',
  action: null,
  approval: { summary: 'Architect wants to comment on GitHub issue 212' },
};

const question = {
  id: 'question:software#13',
  kind: 'question',
  factory: { id: software.id, name: 'software', title: 'Software' },
  since: '2026-10-27T09:30:00Z',
  item: item('software#13', 'Which colour for the badge?'),
  station: 'build',
  action: null,
  approval: null,
};

const toolApproval = {
  id: 'tool:software#14',
  kind: 'tool_approval',
  factory: { id: software.id, name: 'software', title: 'Software' },
  since: '2026-10-27T09:40:00Z',
  item: item('software#14', 'Add the retry'),
  station: 'build',
  action: null,
  approval: null,
};

const expedite = {
  id: 'expedite:software#15',
  kind: 'expedite',
  factory: { id: software.id, name: 'software', title: 'Software' },
  since: '2026-10-27T09:50:00Z',
  item: item('software#15', 'Hotfix'),
  station: 'build',
  action: null,
  approval: null,
};

const alert = {
  id: 'andon:7',
  kind: 'andon',
  factory: { id: sre.id, name: 'sre', title: 'SRE' },
  since: '2026-10-27T10:00:00Z',
  item: null,
  station: null,
  action: null,
  approval: null,
  andon,
};

const board = (stations: unknown[]): Board =>
  ({
    factory: { id: software.id, name: 'software' },
    stations,
    assemblies: [],
    constraint: null,
    constraint_source: null,
    line: {
      wip: { in_progress: 0, limit: null },
      release: { policy: 'pull', in_buffer: 0 },
      expedite: null,
      andon: null,
    },
  }) as unknown as Board;

const station = (id: string, kind: string, waiting: number, inProgress: number, extra = {}) => ({
  id,
  kind,
  waiting,
  in_progress: inProgress,
  agents: [],
  lanes: [],
  items: [],
  busy_ratio: null,
  send_back_ratio: null,
  oldest_waiting_seconds: null,
  done_today: kind === 'done' ? 7 : null,
  ...extra,
});

const softwareBoard = board([
  station('intake', 'queue', 4, 0),
  station('build', 'agent', 6, 2),
  station('done', 'done', 0, 0),
]);

/** Home at 14:00 on a day in October; the seeded queries answer, so no request leaves the test. */
function renderHome({
  can = ['factory:create'],
  factories = overview.factories,
  seedOverview = true,
  inboxList = inboxEntries([decision, alert]),
  boards = {
    [software.id]: softwareBoard,
    [sre.id]: board([station('review', 'agent', 0, 0, { send_back_ratio: 0.4 })]),
    [support.id]: board([station('inbox', 'queue', 9, 0)]),
  },
}: {
  can?: string[];
  factories?: FactoryOverviewList['factories'];
  seedOverview?: boolean;
  inboxList?: Inbox;
  boards?: Record<string, Board>;
} = {}) {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date(2026, 9, 27, 14, 0));
  const seeded = [
    [orgQuery(org.id).queryKey, { ...org, created_at: '2026-10-01T00:00:00Z', can }],
    [
      meQuery.queryKey,
      {
        kind: 'user',
        mode: 'none',
        platform_admin: false,
        user: { id: 'u', name: 'Freddy Cruz', email: 'f@example.com' },
      },
    ],
    [attentionQuery(org.id).queryKey, attention],
    [inboxQuery(org.id).queryKey, inboxList],
    ...(seedOverview
      ? [
          [overviewQuery(org.id).queryKey, { ...overview, factories }] as Seed,
          [
            overviewQuery(org.id, '1d').queryKey,
            {
              ...today,
              factories: factories.flatMap((o) => today.factories.filter((t) => t.factory.id === o.factory.id)),
            },
          ],
        ]
      : []),
    ...Object.entries(boards).flatMap(([id, b]): Seed[] => [
      [boardQuery(org.id, id).queryKey, b] as Seed,
      [
        metricsQuery(org.id, id).queryKey,
        { targets: { throughput_per_day: id === software.id ? 10 : null, lead_time_hours: null } },
      ] as Seed,
    ]),
  ];
  const rendered = renderOrgPage({
    page: () => <FactoriesPage />,
    org,
    seed: seeded as unknown as Seed[],
    topBar: true,
  });
  return rendered;
}

afterEach(() => vi.useRealTimers());

/** Home rendered and its first paint in place: the router settles before the page's queries are read. */
async function renderReady(options?: Parameters<typeof renderHome>[0]) {
  const rendered = renderHome(options);
  await screen.findByRole('heading', { level: 1, name: 'Home' });
  return rendered;
}

describe('Home: the ready state', () => {
  it('titles the page Home and summarises what waits', async () => {
    await renderReady();

    expect(screen.getByRole('heading', { level: 1, name: 'Home' })).toBeTruthy();
    expect(screen.getByText('3 factories, 5 things need you, one andon pulled')).toBeTruthy();
  });

  it('puts New factory and New item in the top bar, once', async () => {
    await renderReady();

    const bar = screen.getByRole('banner');
    expect(within(bar).getByRole('link', { name: 'New factory' })).toBeTruthy();
    expect(within(bar).getByRole('button', { name: /New item/ })).toBeTruthy();
    expect(screen.getAllByRole('link', { name: 'New factory' })).toHaveLength(1);
  });

  it("draws the top bar New item as the normal top-bar button, not the sidebar's full-width row", async () => {
    await renderReady();

    const button = within(screen.getByRole('banner')).getByRole('button', { name: /New item/ });
    expect(button.className).not.toContain('w-full');
    expect(button.querySelector('kbd')).toBeNull();
    expect(button.getAttribute('aria-keyshortcuts')).toBe('C');
  });

  it('keeps the line inside its card: its wrapper has a zero-minimum track and the strip scrolls itself', async () => {
    await renderReady();

    const line = within(screen.getByRole('article', { name: 'software' })).getByRole('region', {
      name: 'software line',
    });
    expect(line.className).toContain('overflow-x-auto');
    const wrapper = line.closest('[aria-busy], .grid');
    expect(wrapper?.className).toContain('grid-cols-[minmax(0,1fr)]');
  });

  it('lists each factory with its health, its line and its three figures', async () => {
    await renderReady();

    const software = screen.getByRole('article', { name: 'software' });
    expect(within(software).getByText('Running')).toBeTruthy();
    expect(within(software).getByRole('region', { name: 'software line' })).toBeTruthy();
    expect(within(software).getByText('Needs you')).toBeTruthy();
    expect(within(software).getByText('Throughput')).toBeTruthy();
    expect(within(software).getByText(/of 10 a day/)).toBeTruthy();
    expect(within(software).getByText('$96.00')).toBeTruthy();

    const sreRow = screen.getByRole('article', { name: 'sre' });
    expect(within(sreRow).getByText('Andon pulled')).toBeTruthy();
    expect(within(sreRow).getByText('Sent back')).toBeTruthy();
    expect(within(sreRow).queryByText('Throughput')).toBeNull();
  });

  it('shows each station of the line as count over name, with Done today', async () => {
    await renderReady();

    const line = screen.getByRole('region', { name: 'software line' });
    expect(line.tabIndex).toBe(0);
    expect(within(line).getByText('Intake')).toBeTruthy();
    expect(within(line).getByText('Done today')).toBeTruthy();
    expect(within(line).queryByText(/merged today/)).toBeNull();
    expect(within(line).queryByText(/Busy/)).toBeNull();
    expect(within(line).queryByText(/in progress/)).toBeNull();
  });

  it('mutes the counts of a stopped line instead of dimming it with opacity, which fails text contrast', async () => {
    await renderReady();

    const stopped = within(screen.getByRole('article', { name: 'sre' })).getByRole('region', { name: 'sre line' });
    expect(stopped.className).not.toContain('opacity');
    expect(stopped.querySelector('.text-muted.text-heading')).not.toBeNull();
    const running = within(screen.getByRole('article', { name: 'software' })).getByRole('region', {
      name: 'software line',
    });
    expect(running.querySelector('.text-muted.text-heading')).toBeNull();
  });

  it('colours the type word: orange for questions, red for alerts, and shows Deny and Approve on a tool approval', async () => {
    await renderReady({ inboxList: inboxEntries([question, expedite, toolApproval]) });

    const needs = screen.getByRole('region', { name: 'Needs you first' });
    expect(within(needs).getByText('Question').className).toContain('text-you-ink');
    expect(within(needs).getByText('Alert').className).toContain('text-rework-ink');
    expect(within(needs).getByText('Approval').className).toContain('text-you-ink');
    expect(within(needs).getByRole('link', { name: 'Reply' }).getAttribute('href')).toBe('/o/acme/needs-you');
    expect(within(needs).getByRole('button', { name: 'Deny' })).toBeTruthy();

    const toolApprove = within(needs).getByRole('button', { name: 'Approve' });
    expect(toolApprove.className).toContain('bg-you');
    expect(toolApprove.className).toContain('text-white');
    expect(toolApprove.className).not.toContain('text-action-ink');
    fireEvent.click(toolApprove);
    await waitFor(() =>
      expect(decideToolApproval).toHaveBeenCalledWith('org-1', 'id-software#14', {
        decision: 'approve',
        always_allow: false,
      }),
    );
  });

  it('denies a tool approval without a note, which the API would refuse empty', async () => {
    await renderReady({ inboxList: inboxEntries([toolApproval]) });
    const needs = screen.getByRole('region', { name: 'Needs you first' });
    fireEvent.click(within(needs).getByRole('button', { name: 'Deny' }));
    await waitFor(() =>
      expect(decideToolApproval).toHaveBeenCalledWith('org-1', 'id-software#14', { decision: 'deny' }),
    );
  });

  it('lists what needs you, with a type and an action', async () => {
    await renderReady();

    const needs = screen.getByRole('region', { name: 'Needs you first' });
    expect(within(needs).getByText('Alert')).toBeTruthy();
    expect(within(needs).getByText('Decision')).toBeTruthy();
    expect(within(needs).getByRole('link', { name: 'Open floor' }).getAttribute('href')).toBe('/o/acme/f/sre');
  });

  it('draws the Needs you first count as an orange 18px pill', async () => {
    await renderReady();

    const needs = screen.getByRole('region', { name: 'Needs you first' });
    const pill = within(needs).getByText('waiting on you').parentElement as HTMLElement;
    expect(pill.className).toContain('rounded-full');
    expect(pill.className).toContain('bg-you');
    expect(pill.className).toContain('h-[18px]');
    expect(pill.className).not.toContain('rounded-chip');
  });

  it('draws every Home factory letter badge at 18px', async () => {
    const { container } = await renderReady();

    const badges = container.querySelectorAll('span[aria-hidden="true"].bg-run');
    expect(badges.length).toBeGreaterThan(0);
    for (const b of badges) expect(b.className).toContain('size-[18px]');
  });

  it('leads Open all from Needs you first to the Needs you page', async () => {
    await renderReady();

    const needs = screen.getByRole('region', { name: 'Needs you first' });
    expect(within(needs).getByRole('link', { name: 'Open all' }).getAttribute('href')).toBe('/o/acme/needs-you');
  });

  it('approves an approval in place, through the item decision', async () => {
    await renderReady();

    const approve = await screen.findByRole('button', { name: 'Approve' });
    expect(approve.className).toContain('bg-you');
    expect(approve.className).toContain('text-white');
    expect(approve.className).not.toContain('text-action-ink');
    fireEvent.click(approve);

    await waitFor(() => expect(approveItem).toHaveBeenCalledWith('org-1', 'software#12'));
  });

  it('sends a denial to the item, where its send-back takes a reason', async () => {
    const { router } = await renderReady();

    fireEvent.click(await screen.findByRole('link', { name: 'Deny' }));

    expect(approveItem).not.toHaveBeenCalled();
    expect(router.state.location.pathname).toContain('/items/');
  });

  it('shows the week against the summed budgets, a bar per factory and the footnote', async () => {
    await renderReady();

    const spend = screen.getByRole('region', { name: 'Spend this week' });
    expect(within(spend).getByText(/\$512/)).toBeTruthy();
    expect(within(spend).getByText(/of \$1,400/)).toBeTruthy();
    expect(within(spend).getByText('Last 7 days')).toBeTruthy();
    const meters = within(spend).getAllByRole('meter');
    expect(meters).toHaveLength(3);
    for (const m of meters) {
      expect(m.className).toContain('bg-hover');
      expect(m.innerHTML).toMatch(/bg-(ink|rework)/);
      expect(m.innerHTML).not.toContain('bg-run');
    }
    expect(spend.querySelector('pattern')).toBeNull();
    expect(within(spend).getAllByRole('meter')).toHaveLength(3);
    expect(within(spend).getByRole('link', { name: 'Spend and billing' }).getAttribute('href')).toBe('/o/acme/spend');
  });

  it('keeps the archived factories one link away', async () => {
    const { router } = await renderReady();

    fireEvent.click(await screen.findByRole('link', { name: 'Archived factories' }));

    expect(router.state.location.search).toEqual({ archived: true });
  });
});

describe('Home: the other states', () => {
  it('shows a loading state until the overview arrives', async () => {
    fake.overview = () => new Promise<never>(() => {});
    renderHome({ seedOverview: false });

    expect(await screen.findByText('Loading')).toBeTruthy();
    expect(document.querySelector('[aria-busy="true"]')).toBeTruthy();
  });

  it('starts a factory when the organization has none, for whoever may create one', async () => {
    renderHome({ factories: [] });

    expect(await screen.findByRole('heading', { level: 2, name: 'No factories in Acme yet' })).toBeTruthy();
  });

  it('says so to a member who may not create one', async () => {
    renderHome({ can: [], factories: [] });

    expect(await screen.findByRole('heading', { level: 2, name: 'No factories to show yet' })).toBeTruthy();
  });

  it('shows the message and a Retry when the overview fails', async () => {
    fake.overview = () => Promise.reject(new Error('goald is not answering'));
    renderHome({ seedOverview: false });

    expect(await screen.findByText('goald is not answering')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Retry' })).toBeTruthy();
  });

  it('leaves only the failed row’s line as a note when its board fails', async () => {
    renderHome({ boards: { [software.id]: softwareBoard } });

    const supportRow = await screen.findByRole('article', { name: 'support' });
    expect(await within(supportRow).findByText(/Line not available/)).toBeTruthy();
    expect(
      within(screen.getByRole('article', { name: 'software' })).getByRole('region', { name: 'software line' }),
    ).toBeTruthy();
  });
});

describe('Home: copy', () => {
  it('summarises with the andon clause only when one is pulled', () => {
    expect(summary(3, 5, 1)).toBe('3 factories, 5 things need you, one andon pulled');
    expect(summary(1, 1, 0)).toBe('1 factory, 1 thing needs you');
    expect(summary(2, 0, 2)).toBe('2 factories, 0 things need you, 2 andons pulled');
  });
});
