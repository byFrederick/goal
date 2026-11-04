// @vitest-environment jsdom

import { cleanup, fireEvent, screen, within } from '@testing-library/react';
import type { ReactNode } from 'react';
import { useState } from 'react';
import { toast } from 'sonner';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  ApiError,
  approveProposal,
  editProposal,
  getJonahOverview,
  getProposal,
  rejectProposal,
} from '../../api/client';
import type { Factory, JonahOverview, OrgSummary, Proposal } from '../../api/types';
import { TopBar, TopBarSetContext, type TopBarState } from '../../app/top-bar';
import { renderFactoryPage } from '../../test/route';
import { TooltipProvider } from '../../ui/tooltip';
import { ProposalPage } from './proposal-page';

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() }, Toaster: () => null }));

vi.mock('../../api/client', async (original) => ({
  ...(await original<typeof import('../../api/client')>()),
  getJonahOverview: vi.fn(),
  getProposal: vi.fn(),
  approveProposal: vi.fn(),
  editProposal: vi.fn(),
  rejectProposal: vi.fn(),
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

const overview: JonahOverview = {
  policy: { enabled: true, propose_after: 5, share: 0.05, budget_usd: 10, approver_roles: ['owner', 'admin'] },
  waiting: 2,
  month_cost_usd: 6.4,
  observations: [],
};

/** Proposal 7 as the picture shows it (proposal.jpg): two files, four sent-back runs, the estimate. */
const proposal7: Proposal = {
  number: 7,
  title: 'Ask frontend-engineer for a screenshot of every UI change',
  state: 'proposed',
  created_at: '2026-10-28T10:00:00Z',
  paths: ['agents/frontend-engineer.md', 'rules/evidence.md'],
  agents: ['frontend-engineer'],
  edits: [],
  estimate: { metric: 'sent_back_rate', station: 'frontend', before: 0.18, after: 0.1, cost_per_item_usd: 0.04 },
  evidence_count: 4,
  summary: 'Four UI changes came back for want of a screenshot.',
  made_against_version: 21,
  publishes_as_version: 22,
  files: [
    {
      path: 'agents/frontend-engineer.md',
      base_sha256: 'a1',
      base_content:
        'Intro\n## Before you open a pull request\n\nRun task check:changed and fix what fails.\nThen open the pull request with gh pr create.',
      content:
        'Intro\n## Before you open a pull request\n\nRun task check:changed and fix what fails.\nFor every change under apps/web, attach a screenshot of each changed screen,\nin the light and the dark theme, to the pull request description.\nThen open the pull request with gh pr create.',
    },
    {
      path: 'rules/evidence.md',
      base_sha256: 'b2',
      base_content: '- Tests: the command you ran and its result.\n- Migrations: proof they apply from zero.',
      content:
        '- Tests: the command you ran and its result.\n- UI changes: a screenshot per changed screen, light and dark.\n- Migrations: proof they apply from zero.',
    },
  ],
  evidence: [
    {
      item_ref: 'software#149',
      item_title: 'Empty state for Intake',
      station: 'review',
      quote: "No screenshot, so I can't check the empty state against the design.",
    },
    {
      item_ref: 'software#138',
      item_title: 'Andon dialog copy and focus order',
      station: 'review',
      quote: 'Show the dialog.',
    },
    {
      item_ref: 'software#131',
      item_title: 'Org switcher hover states',
      station: 'review',
      quote: 'No evidence of that.',
    },
    { item_ref: 'software#127', item_title: 'Board card spacing', station: 'review', quote: 'Attach a screenshot.' },
  ],
};

const tryOf = (verdict: 'fixed' | 'not_fixed', reason?: string) => ({
  attempt: 1,
  state: 'done' as const,
  verdict,
  ...(reason ? { reason } : {}),
  cost_usd: 1,
});

/** The tries of a definition proposal: two items, one fixed and one not, and two kept tests; budget_used leaves them queued. */
function tried(state: 'done' | 'budget_used', queued = false): NonNullable<Proposal['tried']> {
  const queuedTry = { attempt: 1, state: 'queued' as const, cost_usd: 0 };
  return {
    state,
    items: [
      {
        item_ref: 'software#511',
        item_title: 'Board Done cards overflow',
        station: 'frontend',
        tries: [queued ? queuedTry : tryOf('fixed')],
      },
      {
        item_ref: 'software#505',
        item_title: 'One box per column',
        station: 'frontend',
        tries: [queued ? queuedTry : tryOf('not_fixed', 'Zone labels still overflow the track at 768.')],
      },
    ],
    cost_usd: 8.4,
    ...(queued ? {} : { seconds: 41 * 60 }),
    kept_tests: queued
      ? []
      : [
          { test: 418, station: 'frontend', attempt: 1, result: 'passed' as const },
          { test: 493, station: 'frontend', attempt: 1, result: 'failed' as const },
        ],
  };
}

const memoryProposal: Proposal = {
  ...proposal7,
  number: 13,
  title: 'Add 3 facts from answers, notes and send-backs',
  paths: ['memory.yaml'],
  agents: [],
  estimate: undefined as never,
  files: [
    {
      path: 'memory.yaml',
      base_sha256: 'c3',
      base_content: '- text: The pre-push hook runs task check.\n  from: [software#524]\n  added: 2026-11-02\n',
      content:
        '- text: The pre-push hook runs task check.\n  from: [software#524]\n  added: 2026-11-02\n- text: Phones are 390 wide; check 375 too.\n  from: [software#493]\n  added: 2026-11-03\n',
    },
  ],
  evidence: [
    {
      item_ref: 'software#493',
      item_title: 'An answer',
      quote:
        'frontend-engineer asked: “Which phone width should I check?” Freddy Cruz answered: “Phones are 390 wide; check 375 too.”',
    },
  ],
};

const modelTry = (attempt: number, verdict: 'fixed' | 'not_fixed') => ({
  attempt,
  state: 'done' as const,
  verdict,
  cost_usd: 0.2,
});
const modelProposal: Proposal = {
  ...proposal7,
  number: 14,
  title: 'frontend-engineer on claude-haiku-5-5',
  paths: ['factory.yaml'],
  files: [
    {
      path: 'factory.yaml',
      base_sha256: 'd4',
      base_content: '- name: frontend-engineer\n  model: claude-sonnet-5-5\n',
      content: '- name: frontend-engineer\n  model: claude-haiku-5-5\n',
    },
  ],
  tried: {
    state: 'done',
    cost_usd: 4.4,
    seconds: 3900,
    kept_tests: [],
    items: [
      {
        item_ref: 'software#418',
        item_title: 'Needs you header',
        station: 'frontend',
        line_result: 'passed first time, $0.42',
        tries: [modelTry(1, 'fixed'), modelTry(2, 'fixed'), modelTry(3, 'not_fixed')],
      },
      {
        item_ref: 'software#508',
        item_title: 'Item tabs first',
        station: 'frontend',
        line_result: 'sent back 2 times at review, $1.10',
        tries: [modelTry(1, 'not_fixed'), modelTry(2, 'not_fixed'), modelTry(3, 'not_fixed')],
      },
    ],
  },
};

function TopBarFrame({ children }: { children: ReactNode }) {
  const [state, setState] = useState<TopBarState | null>(null);
  return (
    <TopBarSetContext.Provider value={setState}>
      <TopBar state={state} />
      {children}
    </TopBarSetContext.Provider>
  );
}

function renderProposal(factory: Factory = editor) {
  return renderFactoryPage({
    page: () => (
      <TooltipProvider>
        <TopBarFrame>
          <ProposalPage number={7} />
        </TopBarFrame>
      </TooltipProvider>
    ),
    path: 'jonah/proposals/$number',
    location: 'jonah/proposals/7',
    org,
    factory,
  });
}

/** A goald problem as the client throws it, so the page's branch on its code is tested. */
const refusal = (code: string, status = 409) => new ApiError({ title: code, status, code, detail: undefined } as never);

describe('the proposal page, its four states', () => {
  it('is loading until the proposal arrives: the app background, aria-busy and a Loading label', async () => {
    vi.mocked(getJonahOverview).mockReturnValue(new Promise(() => {}));
    vi.mocked(getProposal).mockReturnValue(new Promise(() => {}));
    renderProposal();

    expect(await screen.findByText('Loading')).toBeTruthy();
    expect(document.querySelector('[aria-busy="true"]')).toBeTruthy();
  });

  it('says the proposal does not exist when goald answers proposal-not-found', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    vi.mocked(getProposal).mockRejectedValue(refusal('proposal-not-found', 404));
    renderProposal();

    expect(await screen.findByText('This proposal does not exist')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'All proposals' }).getAttribute('href')).toContain('jonah/proposals');
  });

  it('shows the error state with Try again when the proposal cannot be read', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    vi.mocked(getProposal).mockRejectedValue(new Error('network'));
    renderProposal();

    expect(await screen.findByText('Jonah could not be loaded')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeTruthy();
  });

  it('renders the picture when ready: the chip, the estimate, the diffs, why and the details', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    vi.mocked(getProposal).mockResolvedValue(proposal7);
    renderProposal();

    expect(await screen.findByRole('heading', { level: 1, name: proposal7.title })).toBeTruthy();
    expect(screen.getByText('Waiting for your review')).toBeTruthy();
    expect(screen.getByText(/^Jonah, /)).toBeTruthy();

    const strip = screen.getByRole('region', { name: 'Expected effect' });
    expect(within(strip).getByText('about 10%')).toBeTruthy();
    expect(within(strip).getByText("Now 18%, Jonah's estimate")).toBeTruthy();
    expect(within(strip).getByText('−8 pts').className).toContain('text-ok-ink');
    expect(within(strip).getByText('+$0.04')).toBeTruthy();
    expect(within(strip).getByText('Not tried')).toBeTruthy();

    expect(screen.getByText('2 files, v21 to v22')).toBeTruthy();
    expect(screen.getByRole('region', { name: 'Changes to agents/frontend-engineer.md' })).toBeTruthy();
    expect(
      screen.getByText('For every change under apps/web, attach a screenshot of each changed screen,'),
    ).toBeTruthy();
    expect(screen.getByText('+2')).toBeTruthy();
    expect(screen.getByText('+1')).toBeTruthy();

    const why = screen.getByRole('region', { name: /Why/ });
    expect(within(why).getByText('4 of 4 items')).toBeTruthy();
    expect(within(why).getByText('Empty state for Intake')).toBeTruthy();
    expect(within(why).getByText(/No screenshot, so I can't check/)).toBeTruthy();

    const details = screen.getByRole('button', { name: 'Details' });
    expect(details.getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByText('v22; running items keep v21')).toBeTruthy();
    expect(screen.getByText('Owners and admins')).toBeTruthy();
  });

  it('shows what was tried on past items: each verdict with its reason, the kept tests and the cost', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    vi.mocked(getProposal).mockResolvedValue({ ...proposal7, tried: tried('done') });
    renderProposal();

    const panel = await screen.findByRole('region', { name: /Tried on past items/ });
    expect(within(panel).getByText('41 min, $8.40')).toBeTruthy();
    expect(within(panel).getByText('Fixed')).toBeTruthy();
    expect(within(panel).getByText('Not fixed')).toBeTruthy();
    expect(within(panel).getByText('Zone labels still overflow the track at 768.')).toBeTruthy();
    expect(within(panel).getByText('Passed')).toBeTruthy();
    expect(within(panel).getByText('Failed')).toBeTruthy();
    const strip = screen.getByRole('region', { name: 'Expected effect' });
    expect(within(strip).getByText('1 of 2 fixed')).toBeTruthy();
    expect(within(strip).getByText('1 of 2 kept tests pass')).toBeTruthy();
  });

  it('says Not tried: budget used when the budget ran out before any try', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    vi.mocked(getProposal).mockResolvedValue({ ...proposal7, tried: tried('budget_used', true) });
    renderProposal();

    const panel = await screen.findByRole('region', { name: /Tried on past items/ });
    expect(within(panel).getByText('Not tried: budget used')).toBeTruthy();
    expect(
      within(screen.getByRole('region', { name: 'Expected effect' })).getByText('Not tried: budget used'),
    ).toBeTruthy();
  });

  it('shows a memory proposal with its facts and the quotes they come from', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    vi.mocked(getProposal).mockResolvedValue(memoryProposal);
    renderProposal();

    expect(await screen.findByText('Changes to factory memory')).toBeTruthy();
    const facts = screen.getByRole('region', { name: 'New facts' });
    expect(within(facts).getByText('Phones are 390 wide; check 375 too.')).toBeTruthy();
    expect(within(facts).getByText(/“Which phone width should I check\?”/)).toBeTruthy();
    expect(within(facts).getByText('#493')).toBeTruthy();
    const strip = screen.getByRole('region', { name: 'Expected effect' });
    expect(within(strip).getByText('None')).toBeTruthy();
    expect(screen.queryByRole('region', { name: /Tried on past items/ })).toBeNull();
  });

  it('shows a model proposal with its three tries per item beside the item’s line result', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    vi.mocked(getProposal).mockResolvedValue(modelProposal);
    renderProposal();

    const panel = await screen.findByRole('region', { name: /Tried on past items/ });
    const rows = within(panel).getAllByRole('row');
    expect(within(rows[0] as HTMLElement).getByText('claude-haiku-5-5')).toBeTruthy();
    const first = rows[1] as HTMLElement;
    expect(within(first).getByText('#418')).toBeTruthy();
    expect(within(first).getByText('Passed')).toBeTruthy();
    expect(within(first).getByText(/2 of 3/)).toBeTruthy();
    const second = rows[2] as HTMLElement;
    expect(within(second).getByText('Sent back')).toBeTruthy();
    expect(within(second).getByText(/0 of 3/)).toBeTruthy();
    const strip = screen.getByRole('region', { name: 'Expected effect' });
    expect(within(strip).getByText('2 of 6')).toBeTruthy();
    expect(within(strip).getByText('1 of 2')).toBeTruthy();
  });

  it('a decided proposal shows its state and its reason, and no actions', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    vi.mocked(getProposal).mockResolvedValue({
      ...proposal7,
      state: 'rejected',
      reject_reason: 'Ask for the screenshot in the pull request, not in the rule.',
    });
    renderProposal();

    expect((await screen.findAllByText(/^Rejected/)).length).toBeGreaterThan(0);
    expect(screen.getByText(/Ask for the screenshot in the pull request, not in the rule\./)).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Edit' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Reject with a reason' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Approve and publish v22' })).toBeNull();
  });
});

describe('the decisions', () => {
  it('a viewer without definition:publish sees Approve disabled, and Edit and Reject disabled too', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    vi.mocked(getProposal).mockResolvedValue(proposal7);
    renderProposal(viewer);

    const approve = await screen.findByRole('button', { name: 'Approve and publish v22' });
    expect((approve as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole('button', { name: 'Edit' }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole('button', { name: 'Reject with a reason' }) as HTMLButtonElement).disabled).toBe(true);
  });

  it('Approve shows the draft-open message in plain words and publishes the proposal', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    vi.mocked(getProposal).mockResolvedValue(proposal7);
    vi.mocked(approveProposal).mockRejectedValue(refusal('draft-open'));
    renderProposal();

    fireEvent.click(await screen.findByRole('button', { name: 'Approve and publish v22' }));

    expect(await screen.findByText(/The definition already has an open draft/)).toBeTruthy();
    expect(approveProposal).toHaveBeenCalledWith('org-1', 'factory-1', 7);
  });

  it('Approve swaps its label to a check', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    vi.mocked(getProposal)
      .mockResolvedValueOnce(proposal7)
      .mockResolvedValue({ ...proposal7, state: 'published', published_version: 22 });
    vi.mocked(approveProposal).mockResolvedValue({ number: 7, state: 'published' } as never);
    renderProposal();

    fireEvent.click(await screen.findByRole('button', { name: 'Approve and publish v22' }));

    const done = await screen.findByRole('button', { name: 'Published' });
    expect(done.querySelector('svg')).toBeTruthy();
    expect((done as HTMLButtonElement).disabled).toBe(true);
  });

  it('Edit shows the draft-conflict message when the draft changed the same files', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    vi.mocked(getProposal).mockResolvedValue(proposal7);
    vi.mocked(editProposal).mockRejectedValue(refusal('draft-conflict'));
    renderProposal();

    fireEvent.click(await screen.findByRole('button', { name: 'Edit' }));

    expect(await screen.findByText(/The draft changed these files since Jonah read them/)).toBeTruthy();
    expect(editProposal).toHaveBeenCalledWith('org-1', 'factory-1', 7);
  });

  it('Reject is disabled until a reason that is not blank is typed, then posts it and closes', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    vi.mocked(getProposal).mockResolvedValue(proposal7);
    vi.mocked(rejectProposal).mockResolvedValue({ number: 7, state: 'rejected' });
    renderProposal();

    fireEvent.click(await screen.findByRole('button', { name: 'Reject with a reason' }));
    const dialog = await screen.findByRole('dialog');
    const submit = within(dialog).getByRole('button', { name: 'Reject proposal' }) as HTMLButtonElement;
    expect(submit.disabled).toBe(true);

    const reason = within(dialog).getByRole('textbox', { name: 'Reason' });
    fireEvent.change(reason, { target: { value: '   ' } });
    expect(submit.disabled).toBe(true);

    fireEvent.change(reason, { target: { value: '  Ask for the screenshot in the pull request.  ' } });
    expect(submit.disabled).toBe(false);
    fireEvent.click(submit);

    await vi.waitFor(() =>
      expect(rejectProposal).toHaveBeenCalledWith(
        'org-1',
        'factory-1',
        7,
        'Ask for the screenshot in the pull request.',
      ),
    );
    await vi.waitFor(() => expect(toast.success).toHaveBeenCalledWith(expect.stringContaining('Proposal rejected')));
  });
});
