// @vitest-environment jsdom

import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { approveItem } from '../../api/client';
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
import { FactoriesPage, greeting, summary } from './factories-page';

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
  await screen.findByRole('heading', { level: 1, name: 'Good afternoon, Freddy' });
  return rendered;
}

describe('Home: the ready state', () => {
  it('greets the caller and summarises what waits', async () => {
    await renderReady();

    expect(screen.getByRole('heading', { level: 1, name: 'Good afternoon, Freddy' })).toBeTruthy();
    expect(screen.getByText('3 factories, 5 things need you, one andon pulled')).toBeTruthy();
  });

  it('puts New factory and New item in the top bar, once', async () => {
    await renderReady();

    const bar = screen.getByRole('banner');
    expect(within(bar).getByRole('link', { name: 'New factory' })).toBeTruthy();
    expect(within(bar).getByRole('button', { name: /New item/ })).toBeTruthy();
    expect(screen.getAllByRole('link', { name: 'New factory' })).toHaveLength(1);
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

  it('shows each station of the line with its count and Done today', async () => {
    await renderReady();

    const line = screen.getByRole('region', { name: 'software line' });
    expect(within(line).getByText('Intake')).toBeTruthy();
    expect(within(line).getByText('Done today')).toBeTruthy();
    expect(within(line).getByText('7 items')).toBeTruthy();
  });

  it('lists what needs you, with a type and an action, and never a question', async () => {
    await renderReady();

    const needs = screen.getByRole('region', { name: 'Needs you first' });
    expect(within(needs).getByText('Alert')).toBeTruthy();
    expect(within(needs).getByText('Approval')).toBeTruthy();
    expect(within(needs).getByRole('link', { name: 'Open floor' }).getAttribute('href')).toBe('/o/acme/f/sre');
    expect(screen.queryByText('Question')).toBeNull();
  });

  it('leads Open all from Needs you first to the Needs you page', async () => {
    await renderReady();

    const needs = screen.getByRole('region', { name: 'Needs you first' });
    expect(within(needs).getByRole('link', { name: 'Open all' }).getAttribute('href')).toBe('/o/acme/needs-you');
  });

  it('approves an approval in place, through the item decision', async () => {
    await renderReady();

    fireEvent.click(await screen.findByRole('button', { name: 'Approve' }));

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
    expect(within(spend).getAllByRole('meter')).toHaveLength(3);
  });

  it('renders Spend and billing as not available, and never navigates', async () => {
    const { router } = await renderReady();

    const link = await screen.findByRole('button', { name: 'Spend and billing' });
    expect(link.getAttribute('aria-disabled')).toBe('true');
    fireEvent.click(link);
    expect(router.state.location.pathname).toBe('/o/acme');
  });

  it('keeps the archived factories one control away', async () => {
    const { router } = await renderReady();

    fireEvent.click(await screen.findByRole('radio', { name: 'Archived' }));

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
  it('greets by the time of day and the first name', () => {
    expect(greeting(new Date(2026, 9, 27, 9), 'Freddy Cruz')).toBe('Good morning, Freddy');
    expect(greeting(new Date(2026, 9, 27, 14), undefined)).toBe('Good afternoon');
    expect(greeting(new Date(2026, 9, 27, 20), 'Dana')).toBe('Good evening, Dana');
  });

  it('summarises with the andon clause only when one is pulled', () => {
    expect(summary(3, 5, 1)).toBe('3 factories, 5 things need you, one andon pulled');
    expect(summary(1, 1, 0)).toBe('1 factory, 1 thing needs you');
    expect(summary(2, 0, 2)).toBe('2 factories, 0 things need you, 2 andons pulled');
  });
});
