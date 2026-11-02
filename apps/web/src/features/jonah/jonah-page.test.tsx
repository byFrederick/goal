// @vitest-environment jsdom

import { useSearch } from '@tanstack/react-router';
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { type ReactNode, useState } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError, getJonahOverview, listJudges, listProposals, queueJonahReview } from '../../api/client';
import { jonahOverviewQuery, judgesQuery, proposalsQuery } from '../../api/queries';
import type {
  Factory,
  JonahOverview,
  JudgeList,
  OrgSummary,
  ProposalEntry,
  ProposalPage,
  ProposalState,
} from '../../api/types';
import { TopBar, TopBarSetContext, type TopBarState } from '../../app/top-bar';
import { renderFactoryPage, type Seed } from '../../test/route';
import { TooltipProvider } from '../../ui/tooltip';
import { touchTarget } from '../../ui/touch-target';
import { JonahOverviewPage, JonahProposalsPage } from './jonah-page';

vi.mock('../../api/client', async (original) => ({
  ...(await original<typeof import('../../api/client')>()),
  getJonahOverview: vi.fn(),
  listJudges: vi.fn(),
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

// Two judges: one that scored on the first and the last day of the window, one that is off.
const day = (daysAgo: number, scored: number, passed: number) => ({
  day: new Date(Date.now() - daysAgo * 86_400_000).toISOString().slice(0, 10),
  scored,
  passed,
  pass_rate: passed / scored,
});
const judges: JudgeList = {
  version: 21,
  judge_cost_usd: 1.2,
  judges: [
    {
      slug: 'spec-testable',
      title: 'Spec criteria testable',
      criterion: 'Every criterion can be tested',
      agents: ['architect'],
      sample: 0.25,
      pass_mark: 0.7,
      enabled: true,
      summary: { scored: 10, passed: 9, pass_rate: 0.9, cost_usd: 0.4, days: [day(28, 5, 4), day(0, 5, 5)] },
    },
    {
      slug: 'efficiency',
      title: 'Efficiency',
      criterion: 'No wasted work',
      agents: [],
      sample: 0.5,
      pass_mark: 0.6,
      enabled: false,
      summary: { scored: 0, passed: 0, pass_rate: 0, cost_usd: 0, days: [] },
    },
  ],
};

function listOf(proposals: ProposalEntry[]): ProposalPage {
  return { proposals };
}

function seeds(data: JonahOverview = overview, factory: Factory = editor): Seed[] {
  return [
    [jonahOverviewQuery(org.id, factory.id).queryKey, data],
    [proposalsQuery(org.id, factory.id, 'proposed').queryKey, listOf([proposal8, proposal7])],
    [judgesQuery(org.id, factory.id).queryKey, judges],
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
    expect(screen.getByText('Jonah is not turned on for this installation yet.')).toBeTruthy();
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

  it('links each proposal to its page', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    vi.mocked(listProposals).mockResolvedValue(listOf([proposal8, proposal7]));
    renderOverview();

    await screen.findByRole('heading', { name: 'Waiting for your review' });
    const review = screen.getAllByRole('link', { name: 'Review' });
    expect(review).toHaveLength(2);
    expect(review[0]?.getAttribute('href')).toContain('/jonah/proposals/8');
    expect(review[1]?.getAttribute('href')).toContain('/jonah/proposals/7');
  });
});

describe('the Jonah header', () => {
  it('draws the tabs as links on an underline row: the current one has aria-current', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    renderOverview();

    const nav = await screen.findByRole('navigation', { name: 'Jonah' });
    const links = within(nav).getAllByRole('link');
    expect(links.map((l) => l.textContent?.replace(/\d+$/, ''))).toEqual([
      'Overview',
      'Proposals',
      'Judges',
      'Evals',
      'Bake-offs',
    ]);
    expect(links.filter((l) => l.getAttribute('aria-current') === 'page').map((l) => l.textContent)).toEqual([
      'Overview',
    ]);
    expect(
      within(nav)
        .getByRole('link', { name: /Proposals/ })
        .getAttribute('href'),
    ).toBe('/o/acme/f/software/jonah?tab=proposals');
    expect(within(nav).getByRole('link', { name: 'Judges' }).getAttribute('href')).toBe(
      '/o/acme/f/software/jonah/judges',
    );
    expect(nav.className).toMatch(/border-b/);
  });
});

describe('the pass rate by judge', () => {
  it('shows a chart per enabled judge: its figure, its pass mark, a link and a table for screen readers', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    renderOverview();

    const section = await screen.findByRole('region', { name: 'Pass rate by judge, last 30 days' });
    const charts = within(section).getAllByRole('article');
    expect(charts).toHaveLength(1);
    const chart = charts[0] as HTMLElement;
    expect(within(chart).getByRole('link', { name: 'Spec criteria testable' }).getAttribute('href')).toBe(
      '/o/acme/f/software/jonah/judges/spec-testable',
    );
    expect(within(chart).getByText('90%')).toBeTruthy();
    expect(within(chart).getByText('+20 pts')).toBeTruthy();
    expect(within(chart).getByText('architect, pass at 0.7')).toBeTruthy();
    expect(within(chart).getByRole('img').getAttribute('aria-label')).toBe('From 80% to 100%, pass mark 70%');
    const rects = chart.querySelectorAll('rect');
    expect(rects).toHaveLength(9);
    expect(rects[8]?.getAttribute('class')).toBe('fill-ink');
    expect(rects[0]?.getAttribute('class')).toBe('fill-rule');
    expect(chart.querySelector('line')?.getAttribute('stroke-dasharray')).toBe('4 3');
    expect(within(chart).getByRole('table')).toBeTruthy();
    expect(within(chart).getAllByText('Nothing scored', { selector: 'td' })).toBeTruthy();
    expect(within(section).queryByText('Efficiency')).toBeNull();
  });

  it('is empty without judges', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    renderOverview(editor, [
      ...seeds().filter((entry) => entry[0][2] !== 'list' || entry[0][0] !== 'judges'),
      [judgesQuery(org.id, editor.id).queryKey, { ...judges, judges: [] }],
    ]);

    expect(await screen.findByText('No judges are on yet')).toBeTruthy();
  });

  it('says so when the judges cannot be loaded, and offers Try again', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    vi.mocked(listJudges).mockRejectedValue(new Error('down'));
    renderOverview(editor, seeds().slice(0, 2));

    const section = await screen.findByRole('region', { name: 'Pass rate by judge, last 30 days' });
    expect(await within(section).findByRole('button', { name: 'Try again' })).toBeTruthy();
  });
});

describe('the top bar', () => {
  it('shows the breadcrumb Software › Jonah on the overview', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    vi.mocked(listProposals).mockResolvedValue(listOf([proposal8, proposal7]));
    renderOverview();

    await screen.findByRole('heading', { name: 'Waiting for your review' });
    const crumbs = screen.getByRole('navigation', { name: /breadcrumb/i });
    expect(within(crumbs).getByText('Software')).toBeTruthy();
    expect(within(crumbs).getAllByText('Jonah')).toHaveLength(1);
  });
});

describe('Look for improvements now', () => {
  it('is a secondary button that is not available yet, with no explanation beside it, and sends no request', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    vi.mocked(listProposals).mockResolvedValue(listOf([proposal8, proposal7]));
    renderOverview();

    await screen.findByRole('heading', { name: 'Waiting for your review' });
    const look = screen.getByRole('button', { name: 'Look for improvements now' });
    expect(look.getAttribute('aria-disabled')).toBe('true');
    expect(look.className).toContain('border-rule');
    expect(screen.queryByText("Reviews start when Jonah's review loop ships")).toBeNull();
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

describe('phone tap targets on the Jonah policy', () => {
  it('gives both Not available yet buttons the touchTarget classes', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    vi.mocked(listProposals).mockResolvedValue(listOf([proposal8, proposal7]));
    renderOverview();
    const policy = await screen.findByRole('region', { name: 'How Jonah works here' });
    const buttons = within(policy).getAllByRole('button', { name: 'Not available yet' });
    expect(buttons).toHaveLength(2);
    for (const button of buttons)
      for (const cls of touchTarget.split(' ')) expect(button.classList.contains(cls)).toBe(true);
  });
});
