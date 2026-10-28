// @vitest-environment jsdom

import { useSearch } from '@tanstack/react-router';
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { type ReactNode, useState } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError, getJonahOverview, listProposals, queueJonahReview } from '../../api/client';
import { jonahOverviewQuery, proposalsQuery } from '../../api/queries';
import type { Factory, JonahOverview, OrgSummary, ProposalEntry, ProposalPage, ProposalState } from '../../api/types';
import { TopBar, TopBarSetContext, type TopBarState } from '../../app/top-bar';
import { renderFactoryPage, type Seed } from '../../test/route';
import { TooltipProvider } from '../../ui/tooltip';
import { JonahOverviewPage, JonahProposalsPage } from './jonah-page';

vi.mock('../../api/client', async (original) => ({
  ...(await original<typeof import('../../api/client')>()),
  getJonahOverview: vi.fn(),
  listProposals: vi.fn(),
  queueJonahReview: vi.fn(),
}));

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
  title: 'Split requests over 400 changed lines into an assembly',
  state: 'proposed',
  created_at: '2026-10-27T12:00:00Z',
  paths: ['agents/architect.md'],
  agents: ['architect'],
  estimate: { metric: 'lead_time_p85', station: '', before: 31, after: 26, cost_per_item_usd: 0 },
  evidence_count: 6,
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

const overview: JonahOverview = {
  policy: {
    enabled: true,
    propose_after: 5,
    weekly_usd: 20,
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
    },
  ],
};

const empty: JonahOverview = { ...overview, waiting: 0, observations: [], month_cost_usd: 0 };

function listOf(proposals: ProposalEntry[]): ProposalPage {
  return { proposals };
}

function seeds(data: JonahOverview = overview, factory: Factory = editor): Seed[] {
  return [
    [jonahOverviewQuery(org.id, factory.id).queryKey, data],
    [proposalsQuery(org.id, factory.id, 'proposed').queryKey, listOf([proposal8, proposal7])],
  ];
}

// The page with the shell's top bar above it, so its TopBarSlot actions are on screen as they are in the app.
function TopBarFrame({ children }: { children: ReactNode }) {
  const [state, setState] = useState<TopBarState | null>(null);
  return (
    <TopBarSetContext.Provider value={setState}>
      <TopBar state={state} />
      {children}
    </TopBarSetContext.Provider>
  );
}

function renderOverview(factory: Factory = editor, seed: Seed[] = seeds(overview, factory)) {
  return renderFactoryPage({
    page: () => (
      <TooltipProvider>
        <TopBarFrame>
          <JonahOverviewPage />
        </TopBarFrame>
      </TooltipProvider>
    ),
    path: 'jonah/',
    org,
    factory,
    seed,
  });
}

// The proposals tab reads its state from the URL, as the route does, so a filter changes the list it asks for.
function ProposalsRoute() {
  const { state } = useSearch({ strict: false }) as { state?: ProposalState };
  return (
    <TooltipProvider>
      <TopBarFrame>
        <JonahProposalsPage state={state} />
      </TopBarFrame>
    </TooltipProvider>
  );
}

function renderProposals(search: Record<string, string>, factory: Factory = editor) {
  return renderFactoryPage({
    page: () => <ProposalsRoute />,
    path: 'jonah/',
    org,
    factory,
    search: { tab: 'proposals', ...search },
    seed: [[proposalsQuery(org.id, factory.id).queryKey, listOf([applied6, proposal8, proposal7])]],
  });
}

describe('the Jonah overview, its four states', () => {
  it('is loading until the overview arrives: the app background, aria-busy and a Loading label', async () => {
    vi.mocked(getJonahOverview).mockReturnValue(new Promise(() => {}));
    renderOverview(editor, []);

    const busy = (await screen.findByText('Loading')).closest('[aria-busy]');
    expect(busy?.getAttribute('aria-busy')).toBe('true');
    expect(screen.queryByRole('heading', { name: 'Waiting for your review' })).toBeNull();
  });

  it('says Jonah is not available on a goald without it, and offers to try again', async () => {
    vi.mocked(getJonahOverview).mockRejectedValue(
      new ApiError({ title: 'Jonah unavailable', status: 503, code: 'jonah-unavailable' } as never),
    );
    renderOverview(editor, []);

    expect(await screen.findByText('Jonah could not be loaded')).toBeTruthy();
    expect(screen.getByText('Jonah is not available on this goald yet.')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeTruthy();
  });

  it('is empty when nothing waits and nothing was noticed', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(empty);
    renderOverview(editor, seeds(empty).slice(0, 1));

    expect(await screen.findByText('Jonah has nothing to review yet')).toBeTruthy();
    expect(screen.queryByRole('heading', { name: 'What Jonah noticed this week' })).toBeNull();
  });

  it('is ready: the waiting proposals, the policy in words and what Jonah noticed', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    vi.mocked(listProposals).mockResolvedValue(listOf([proposal8, proposal7]));
    renderOverview();

    const waiting = await screen.findByRole('heading', { name: 'Waiting for your review' });
    expect(waiting).toBeTruthy();
    expect(screen.getByRole('link', { name: 'All proposals' }).getAttribute('href')).toBe(
      '/o/acme/f/software/jonah?tab=proposals',
    );

    const first = screen.getByRole('article', { name: /Ask frontend-engineer/ });
    expect(within(first).getByText('Proposal 7')).toBeTruthy();
    expect(within(first).getByText('frontend-engineer')).toBeTruthy();
    expect(within(first).getByText('rules/evidence.md')).toBeTruthy();
    expect(within(first).getByText(/18%/)).toBeTruthy();
    expect(within(first).getByText(/about 10%/)).toBeTruthy();
    expect(within(first).getByText('+$0.04')).toBeTruthy();
    expect(within(first).getByText('Based on 4 runs sent back')).toBeTruthy();

    const second = screen.getByRole('article', { name: /Split requests/ });
    expect(within(second).getByText(/31 h/)).toBeTruthy();
    expect(within(second).getByText('no change')).toBeTruthy();

    const policy = screen.getByRole('region', { name: 'How Jonah works here' });
    expect(within(policy).getByText('5 failures with the same cause')).toBeTruthy();
    expect(within(policy).getByText('Owners and admins')).toBeTruthy();
    expect(within(policy).getByText('claude-opus-5-5')).toBeTruthy();
    expect(within(policy).getByText('$6.40 this month')).toBeTruthy();
    expect(within(policy).getByRole('link', { name: 'Change' }).getAttribute('href')).toBe(
      '/o/acme/f/software/definition/basics?mode=yaml&file=factory.yaml',
    );

    const noticed = screen.getByRole('region', { name: 'What Jonah noticed this week' });
    expect(within(noticed).getByText('Proposed')).toBeTruthy();
    expect(within(noticed).getByText('Watching, 3 of 5')).toBeTruthy();
  });

  it('links each proposal to its page, shows the Evals control as not available, and no pass rates', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    vi.mocked(listProposals).mockResolvedValue(listOf([proposal8, proposal7]));
    renderOverview();

    await screen.findByRole('heading', { name: 'Waiting for your review' });
    const review = screen.getAllByRole('link', { name: 'Review' });
    expect(review).toHaveLength(2);
    expect(review[0]?.getAttribute('href')).toContain('/jonah/proposals/8');
    expect(review[1]?.getAttribute('href')).toContain('/jonah/proposals/7');
    expect(
      screen.getAllByRole('button', { name: 'Evals' }).every((b) => b.getAttribute('aria-disabled') === 'true'),
    ).toBe(true);
    expect(screen.getByText('Pass rate by judge, last 30 days')).toBeTruthy();
    expect(screen.getByText('Not available yet', { selector: 'p' })).toBeTruthy();
    expect(screen.queryByText(/\d+%/, { selector: '.num' })).toBeNull();
  });
});

describe('Look for improvements now', () => {
  it('queues a review and reads the Jonah keys again', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    vi.mocked(listProposals).mockResolvedValue(listOf([proposal8, proposal7]));
    vi.mocked(queueJonahReview).mockResolvedValue({
      id: 'review-1',
      state: 'pending',
      trigger: 'manual',
      created_at: '2026-10-28T12:00:00Z',
    });
    const { queryClient } = renderOverview();

    await screen.findByRole('heading', { name: 'Waiting for your review' });
    fireEvent.click(screen.getByRole('button', { name: 'Look for improvements now' }));

    expect(await screen.findByText('Review queued. Its proposals appear here when it finishes.')).toBeTruthy();
    expect(queueJonahReview).toHaveBeenCalledWith('org-1', 'factory-1');
    // The review's success invalidates the Jonah keys, so the mounted overview is read again.
    await waitFor(() => expect(getJonahOverview).toHaveBeenCalledWith('org-1', 'factory-1', expect.anything()));
    expect(queryClient.getQueryData(jonahOverviewQuery(org.id, editor.id).queryKey)).toEqual(overview);
  });

  it('shows a 409 review-in-flight as a plain sentence', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    vi.mocked(listProposals).mockResolvedValue(listOf([proposal8, proposal7]));
    vi.mocked(queueJonahReview).mockRejectedValue(
      new ApiError({ title: 'Review in flight', status: 409, code: 'review-in-flight' } as never),
    );
    renderOverview();

    await screen.findByRole('heading', { name: 'Waiting for your review' });
    fireEvent.click(screen.getByRole('button', { name: 'Look for improvements now' }));

    expect(
      await screen.findByText('A review is already running. Its proposals appear here when it finishes.'),
    ).toBeTruthy();
  });

  it('shows a 409 learning-budget-exhausted as a plain sentence', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    vi.mocked(listProposals).mockResolvedValue(listOf([]));
    vi.mocked(queueJonahReview).mockRejectedValue(
      new ApiError({ title: 'Budget used', status: 409, code: 'learning-budget-exhausted' } as never),
    );
    renderOverview();

    await screen.findByRole('heading', { name: 'Waiting for your review' });
    fireEvent.click(screen.getByRole('button', { name: 'Look for improvements now' }));

    expect(await screen.findByText(/has used its weekly budget for Jonah/)).toBeTruthy();
  });

  it('is disabled without definition:edit and never calls the API', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    vi.mocked(listProposals).mockResolvedValue(listOf([]));
    renderOverview(viewer, seeds(overview, viewer));

    await screen.findByRole('heading', { name: 'Waiting for your review' });
    const look = screen.getByRole('button', { name: 'Look for improvements now' });
    expect((look as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(look);
    await waitFor(() => expect(queueJonahReview).not.toHaveBeenCalled());
  });
});

describe('the Proposals tab', () => {
  it('lists every proposal when no state is named, and the state the URL names when it is', async () => {
    const { router } = renderProposals({});
    expect(await screen.findByRole('radio', { name: 'All' })).toBeTruthy();
    const applied = screen.getByRole('article', { name: /Name the agent in every sent-back note/ });
    expect(within(applied).getByText('Applied')).toBeTruthy();

    fireEvent.click(screen.getByRole('radio', { name: 'Waiting' }));
    await waitFor(() => expect(router.state.location.search).toMatchObject({ state: 'proposed' }));
  });

  it('says so when a state has no proposals', async () => {
    renderFactoryPage({
      page: () => <ProposalsRoute />,
      path: 'jonah/',
      org,
      factory: editor,
      search: { tab: 'proposals', state: 'rejected' },
      seed: [[proposalsQuery(org.id, editor.id, 'rejected').queryKey, listOf([])]],
    });

    expect(await screen.findByText('No proposals in this state')).toBeTruthy();
  });
});
