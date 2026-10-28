// @vitest-environment jsdom

import { cleanup, fireEvent, screen, within } from '@testing-library/react';
import type { ReactNode } from 'react';
import { useState } from 'react';
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
  policy: { enabled: true, propose_after: 5, weekly_usd: 20, approver_roles: ['owner', 'admin'] },
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
    expect(screen.getByRole('link', { name: 'All proposals' }).getAttribute('href')).toContain('jonah?tab=proposals');
  });

  it('shows the error state with Try again when the proposal cannot be read', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    vi.mocked(getProposal).mockRejectedValue(new Error('network'));
    renderProposal();

    expect(await screen.findByText('Jonah could not be loaded')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeTruthy();
  });

  it('renders the picture when ready: the chip, the estimate, the diffs, the runs and the details', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    vi.mocked(getProposal).mockResolvedValue(proposal7);
    renderProposal();

    expect(await screen.findByRole('heading', { level: 1, name: proposal7.title })).toBeTruthy();
    expect(screen.getByText('Waiting for your review')).toBeTruthy();
    expect(screen.getByText(/Proposed by Jonah .* after 4 runs sent back\. Rejecting tells Jonah why/)).toBeTruthy();

    expect(screen.getByText('18%').tagName).toBe('S');
    expect(screen.getByText('about 10%')).toBeTruthy();
    expect(screen.getByText('Estimated from the 4 runs and their reasons')).toBeTruthy();
    expect(screen.getByText('+$0.04')).toBeTruthy();

    expect(screen.getByText('2 files, v21 to v22')).toBeTruthy();
    expect(screen.getByRole('region', { name: 'Changes to agents/frontend-engineer.md' })).toBeTruthy();
    expect(
      screen.getByText('For every change under apps/web, attach a screenshot of each changed screen,'),
    ).toBeTruthy();
    expect(screen.getByText('+2')).toBeTruthy();
    expect(screen.getByText('+1')).toBeTruthy();

    expect(screen.getByText('Why: 4 runs sent back')).toBeTruthy();
    expect(screen.getByText('Empty state for Intake')).toBeTruthy();
    expect(screen.getByText(/No screenshot, so I can't check/)).toBeTruthy();

    expect(screen.getByText('v22; runs in progress keep v21')).toBeTruthy();
    expect(screen.getByText('Owners and admins')).toBeTruthy();
  });

  it('shows the not-built controls as not available: the eval tile, Found by, Tested on and Eval results', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    vi.mocked(getProposal).mockResolvedValue(proposal7);
    renderProposal();

    await screen.findByRole('heading', { level: 1 });
    const notAvailable = [
      screen.getByRole('button', { name: /New eval case/ }),
      screen.getByRole('button', { name: 'The judge' }),
      ...screen.getAllByRole('button', { name: 'Not available yet' }),
    ];
    for (const control of notAvailable) expect(control.getAttribute('aria-disabled')).toBe('true');
    expect(notAvailable).toHaveLength(4);
  });

  it('a decided proposal shows its state and its reason, and no actions', async () => {
    vi.mocked(getJonahOverview).mockResolvedValue(overview);
    vi.mocked(getProposal).mockResolvedValue({
      ...proposal7,
      state: 'rejected',
      reject_reason: 'Ask for the screenshot in the pull request, not in the rule.',
    });
    renderProposal();

    expect(await screen.findByText('Rejected')).toBeTruthy();
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
  });
});
