// @vitest-environment jsdom

import { useSearch } from '@tanstack/react-router';
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { toast } from 'sonner';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError, getJonahOverview, queueJonahReview, undoProposal } from '../../api/client';
import {
  jonahOverviewQuery,
  keptTestsQuery,
  membersQuery,
  proposalQuery,
  proposalsQuery,
  versionsQuery,
} from '../../api/queries';
import type {
  Factory,
  JonahOverview,
  OrgSummary,
  Proposal,
  ProposalEntry,
  ProposalPage,
  ProposalState,
  Version,
  Watch,
} from '../../api/types';
import { renderFactoryPage, type Seed } from '../../test/route';
import { JonahOverviewPage, JonahProposalsPage } from './jonah-page';

vi.mock('../../api/client', async (original) => ({
  ...(await original<typeof import('../../api/client')>()),
  getJonahOverview: vi.fn(),
  queueJonahReview: vi.fn(),
  undoProposal: vi.fn(),
}));
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'Ac', factory_count: 1, role: 'owner' };

const editor: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'software',
  title: 'Software',
  work_types: ['feature'],
  stations: [],
  current: { id: 'version-21', number: 21 } as unknown as Factory['current'],
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'editor',
  can: ['definition:edit', 'definition:publish', 'factory:read'],
};
const viewer: Factory = { ...editor, role: 'viewer', can: ['factory:read'] };

const proposal7: ProposalEntry = {
  number: 7,
  title: 'Ask frontend-engineer for a screenshot of every UI change',
  state: 'proposed',
  created_at: '2026-10-28T10:00:00Z',
  paths: ['rules/evidence.md'],
  agents: ['frontend-engineer'],
  estimate: { metric: 'sent_back_rate', station: 'frontend', before: 0.18, after: 0.1, cost_per_item_usd: 0.04 },
  evidence_count: 4,
};
const proposal8: ProposalEntry = {
  number: 8,
  title: 'Add 3 facts from answers, notes and send-backs',
  state: 'proposed',
  created_at: '2026-10-27T12:00:00Z',
  paths: ['memory.yaml'],
  agents: [],
  evidence_count: 3,
};
const applied6: ProposalEntry = {
  number: 6,
  title: 'Name the agent in every sent-back note',
  state: 'applied',
  created_at: '2026-10-19T12:00:00Z',
  paths: ['agents/reviewer.md'],
  agents: ['reviewer'],
  evidence_count: 3,
};

const full7 = {
  ...proposal7,
  summary: '',
  edits: [],
  files: [],
  evidence: [],
  made_against_version: 21,
  publishes_as_version: 22,
  tried: {
    state: 'done',
    items: [
      {
        item_ref: 'software#511',
        item_title: 'Board',
        station: 'frontend',
        tries: [{ attempt: 1, state: 'done', verdict: 'fixed', cost_usd: 1 }],
      },
      {
        item_ref: 'software#505',
        item_title: 'Box',
        station: 'frontend',
        tries: [{ attempt: 1, state: 'done', verdict: 'not_fixed', cost_usd: 1 }],
      },
    ],
    cost_usd: 2,
    kept_tests: [{ test: 418, result: 'passed' }],
  },
} as Proposal;
const full8 = {
  ...proposal8,
  summary: '',
  edits: [],
  files: [],
  evidence: [
    { item_ref: 'software#493', item_title: 'Phones', quote: 'Phones are 390 wide; check 375 too.' },
    { item_ref: 'software#371', item_title: 'Docs', quote: 'The docs site is apps/docs.' },
    { item_ref: 'software#308', item_title: 'ADR', quote: 'Check main before you number one.' },
  ],
  made_against_version: 21,
  publishes_as_version: 22,
} as Proposal;

const overview: JonahOverview = {
  policy: {
    enabled: true,
    propose_after: 5,
    share: 0.05,
    budget_usd: 14,
    model: 'claude-opus-5-5',
    approver_roles: ['owner', 'admin'],
  },
  waiting: 2,
  month_cost_usd: 6.4,
  observations: [
    {
      id: 'obs-1',
      summary: 'reviewer sent back 4 UI changes because they had no screenshot',
      detail: '#149, #138, #131, #127',
      item_refs: [],
      state: 'proposed',
      count: 5,
      propose_after: 5,
      proposal: 7,
      created_at: '2026-10-28T10:00:00Z',
      updated_at: '2026-10-28T10:00:00Z',
    },
    {
      id: 'obs-2',
      summary: 'backend-engineer ran the whole test suite where the changed checks were enough',
      detail: '9 runs, about $0.60 more each',
      item_refs: [],
      state: 'watching',
      count: 3,
      propose_after: 5,
      created_at: '2026-10-27T10:00:00Z',
      updated_at: '2026-10-27T10:00:00Z',
    },
  ],
};
const empty: JonahOverview = { ...overview, waiting: 0, observations: [] };

const watch = (over: Partial<Watch>): Watch => ({
  station: 'architect',
  metric: 'lead_time_p85',
  before: { items: 40, value: 31 * 3600 },
  after: { items: 64, value: 34 * 3600 },
  interval: { low: 1, high: 2 },
  verdict: 'worse',
  opened_at: '2026-10-20T00:00:00Z',
  closed_at: null,
  ...over,
});
const version = (number: number, message: string, watches: Watch[]): Version =>
  ({
    id: `v-${number}`,
    number,
    message,
    published_by: { kind: 'user', id: 'u1', name: 'Freddy Cruz' },
    watches,
  }) as unknown as Version;
const versions = {
  versions: [
    version(40, 'Architect splits specs over 400 changed lines', [watch({ proposal: 11 })]),
    version(39, 'frontend-engineer on Sonnet 5.5', [
      watch({
        station: 'frontend',
        metric: 'cost_per_item',
        verdict: 'helped',
        before: { items: 10, value: 2.38 },
        after: { items: 30, value: 1.91 },
      }),
    ]),
    version(38, 'Docs', [
      watch({
        proposal: 10,
        station: 'integrate',
        metric: 'sent_back_rate',
        before: { items: 9, value: 0.09 },
        after: { items: 112, value: 0.2 },
      }),
    ]),
  ],
  next_before: null,
};

const page = (proposals: ProposalEntry[]): ProposalPage => ({ proposals });

function seeds(factory: Factory = editor, data: JonahOverview = overview): Seed[] {
  return [
    [jonahOverviewQuery(org.id, factory.id).queryKey, data],
    [proposalsQuery(org.id, factory.id, 'proposed').queryKey, page([proposal8, proposal7])],
    [proposalsQuery(org.id, factory.id).queryKey, page([proposal8, proposal7, applied6])],
    [proposalQuery(org.id, factory.id, 7).queryKey, full7],
    [proposalQuery(org.id, factory.id, 8).queryKey, full8],
    [versionsQuery(org.id, factory.id).queryKey, versions],
    [keptTestsQuery(org.id, factory.id).queryKey, { tests: [] }],
    [membersQuery(org.id).queryKey, []],
  ];
}

function renderOverview(factory: Factory = editor, seed: Seed[] = seeds(factory)) {
  return renderFactoryPage({ page: () => <JonahOverviewPage />, path: 'jonah/', org, factory, seed, topBar: true });
}

// The Proposals tab reads its state from the URL, as the route does, so a filter changes the list it asks for.
function ProposalsRoute() {
  const { state } = useSearch({ strict: false }) as { state?: ProposalState };
  return <JonahProposalsPage state={state} />;
}

describe('the Jonah overview, its four states', () => {
  it('is loading until the overview arrives: aria-busy and a Loading label', async () => {
    vi.mocked(getJonahOverview).mockReturnValue(new Promise(() => {}));
    renderOverview(editor, []);

    const busy = (await screen.findAllByText('Loading'))[0]?.closest('[aria-busy]');
    expect(busy?.getAttribute('aria-busy')).toBe('true');
    expect(screen.queryByRole('region', { name: 'How Jonah works here' })).toBeNull();
  });

  it('says Jonah is not available on a goald without it, and offers to try again', async () => {
    vi.mocked(getJonahOverview).mockRejectedValue(
      new ApiError({ title: 'Jonah unavailable', status: 503, code: 'jonah-unavailable' } as never),
    );
    renderOverview(editor, []);

    expect((await screen.findAllByText('Jonah could not be loaded')).length).toBeGreaterThan(0);
    expect(screen.getAllByText('Jonah is not turned on for this installation yet.').length).toBeGreaterThan(0);
    expect(screen.getAllByRole('button', { name: 'Try again' }).length).toBeGreaterThan(0);
  });

  it('is empty when nothing waits and nothing was noticed', async () => {
    renderOverview(editor, [
      [jonahOverviewQuery(org.id, editor.id).queryKey, empty],
      [proposalsQuery(org.id, editor.id, 'proposed').queryKey, page([])],
      [proposalsQuery(org.id, editor.id).queryKey, page([])],
      [versionsQuery(org.id, editor.id).queryKey, { versions: [], next_before: null }],
      [keptTestsQuery(org.id, editor.id).queryKey, { tests: [] }],
    ]);

    expect(await screen.findByText(/Nothing waits for you/)).toBeTruthy();
    expect(screen.getByText('Nothing published is watched yet.')).toBeTruthy();
    expect(screen.getByText(/Nothing yet\./)).toBeTruthy();
  });

  it('shows each panel: waiting proposals, After publish, what Jonah noticed and how Jonah works here', async () => {
    renderOverview();

    expect(await screen.findByRole('heading', { name: 'Waiting for your review' })).toBeTruthy();
    const first = screen.getByRole('article', { name: /Add 3 facts/ });
    expect(within(first).getByText('memory.yaml')).toBeTruthy();
    expect(within(first).getByText(/Phones are 390 wide/)).toBeTruthy();
    expect(within(first).getByText(/and 2 more/)).toBeTruthy();
    expect(within(first).getByText("Quoted from 3 people's words")).toBeTruthy();

    const second = screen.getByRole('article', { name: /Ask frontend-engineer/ });
    expect(within(second).getByText(/18%/)).toBeTruthy();
    expect(within(second).getByText(/about 10%/)).toBeTruthy();
    expect(within(second).getByText('Tried: fixed 1 of 2, kept tests 1 of 1')).toBeTruthy();
    expect(within(second).getByText('+$0.04')).toBeTruthy();

    const policy = screen.getByRole('region', { name: /How Jonah works here/ });
    expect(within(policy).getByText('5 failures with the same cause')).toBeTruthy();
    expect(within(policy).getByText('Owners and admins')).toBeTruthy();
    expect(within(policy).getByText('claude-opus-5-5')).toBeTruthy();
    expect(within(policy).getByRole('link', { name: 'Change' }).getAttribute('href')).toBe(
      '/o/acme/f/software/definition/basics?mode=yaml&file=factory.yaml',
    );

    const noticed = screen.getByRole('region', { name: 'What Jonah noticed this week' });
    expect(within(noticed).getByText('Proposed')).toBeTruthy();
    expect(within(noticed).getByText('Watching, 3 of 5')).toBeTruthy();
  });

  it('shows the tooltip of how Jonah works behind its i button', async () => {
    renderOverview();
    const info = await screen.findByRole('button', { name: 'How Jonah works' });
    info.focus();
    expect(await screen.findByText(/Jonah reviews the line once a day/)).toBeTruthy();
  });

  it('links each proposal to its page', async () => {
    renderOverview();

    await screen.findByRole('heading', { name: 'Waiting for your review' });
    const review = screen.getAllByRole('link', { name: /^Review/ });
    expect(review).toHaveLength(2);
    expect(review[0]?.getAttribute('href')).toContain('/jonah/proposals/8');
    expect(review[1]?.getAttribute('href')).toContain('/jonah/proposals/7');
  });
});

describe('After publish', () => {
  it('shows each change before and after with its verdict, and Undo this change only on a Worse watch of a proposal', async () => {
    renderOverview();

    const panel = await screen.findByRole('region', { name: /After publish/ });
    expect(within(panel).getByText('Lead time p85 at architect: 31 h → 34 h')).toBeTruthy();
    expect(within(panel).getByText('Cost per item at frontend: $2.38 → $1.91')).toBeTruthy();
    expect(within(panel).getByText('Proposal 11, v40')).toBeTruthy();
    expect(within(panel).getByText('v39, by Freddy Cruz')).toBeTruthy();
    expect(within(panel).getByText('Helped')).toBeTruthy();
    expect(within(panel).getAllByText('Worse')).toHaveLength(2);
    expect(within(panel).getAllByRole('button', { name: /Undo this change/ })).toHaveLength(2);
  });

  it('undoes a proposal and opens the proposal that reverses it', async () => {
    vi.mocked(undoProposal).mockResolvedValue({ number: 15 } as Proposal);
    const { router } = renderOverview();

    const panel = await screen.findByRole('region', { name: /After publish/ });
    fireEvent.click(within(panel).getAllByRole('button', { name: /Undo this change/ })[0] as HTMLElement);

    await waitFor(() => expect(undoProposal).toHaveBeenCalledWith('org-1', 'factory-1', 11));
    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/f/software/jonah/proposals/15'));
  });

  it('offers no Undo to a person who cannot edit the definition', async () => {
    renderOverview(viewer);

    const panel = await screen.findByRole('region', { name: /After publish/ });
    expect(within(panel).getAllByText('Worse')).toHaveLength(2);
    expect(within(panel).queryByRole('button', { name: /Undo this change/ })).toBeNull();
  });
});

describe('Look for improvements now', () => {
  it('queues a review', async () => {
    vi.mocked(queueJonahReview).mockResolvedValue({ id: 'r1' } as never);
    renderOverview();

    fireEvent.click(await screen.findByRole('button', { name: 'Look for improvements now' }));

    await waitFor(() => expect(queueJonahReview).toHaveBeenCalledWith('org-1', 'factory-1'));
    await waitFor(() => expect(toast.success).toHaveBeenCalled());
  });

  it.each([
    ['review-in-flight', 'A review is already running. Its proposals appear here.'],
    ['learning-budget-exhausted', "Jonah's budget for this week is used up, so no review was started."],
  ])('says what a 409 %s means', async (code, message) => {
    vi.mocked(queueJonahReview).mockRejectedValue(new ApiError({ title: 'Conflict', status: 409, code } as never));
    renderOverview();

    fireEvent.click(await screen.findByRole('button', { name: 'Look for improvements now' }));

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith(message));
  });

  it('is disabled without definition:edit', async () => {
    renderOverview(viewer);
    const button = await screen.findByRole('button', { name: 'Look for improvements now' });
    expect((button as HTMLButtonElement).disabled).toBe(true);
  });
});

describe('the Proposals tab', () => {
  function renderProposals(search: Record<string, string> = {}) {
    return renderFactoryPage({
      page: () => <ProposalsRoute />,
      path: 'jonah/proposals/',
      org,
      factory: editor,
      search,
      seed: [...seeds(), [proposalsQuery(org.id, editor.id, 'rejected').queryKey, page([])]],
    });
  }

  it('lists every proposal, newest first, with its state', async () => {
    renderProposals();

    expect(await screen.findByRole('article', { name: /Name the agent/ })).toBeTruthy();
    expect(screen.getAllByRole('article')).toHaveLength(3);
    expect(within(screen.getByRole('article', { name: /Name the agent/ })).getByText('Applied')).toBeTruthy();
  });

  it('filters by state in the URL, and says when none is in that state', async () => {
    const { router } = renderProposals({ state: 'rejected' });

    expect(await screen.findByText('No proposals in this state')).toBeTruthy();
    fireEvent.click(screen.getByRole('radio', { name: 'All' }));
    await waitFor(() => expect(router.state.location.search).not.toHaveProperty('state'));
  });
});
