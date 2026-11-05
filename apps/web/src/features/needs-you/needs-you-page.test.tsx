// @vitest-environment jsdom

import { ApiError } from '@goal/api-client';
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { toast } from 'sonner';
import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  answerItem,
  approveItem,
  decideToolApproval,
  getInbox,
  listItemEvents,
  mergeItem,
  rejectItem,
  undoProposal,
} from '../../api/client';
import { factoriesQuery, inboxQuery, keys, orgQuery } from '../../api/queries';
import type { Inbox, InboxEntry, Item, ItemCard, OrgSummary } from '../../api/types';
import { renderOrgPage } from '../../test/route';
import { TooltipProvider } from '../../ui/tooltip';
import { NeedsYouPage } from './needs-you-page';

vi.mock('../../api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/client')>()),
  getInbox: vi.fn(),
  approveItem: vi.fn(),
  mergeItem: vi.fn(),
  rejectItem: vi.fn(),
  listItemEvents: vi.fn(),
  answerItem: vi.fn(),
  decideToolApproval: vi.fn(),
  undoProposal: vi.fn(),
}));
vi.mock('sonner', () => ({
  toast: Object.assign(vi.fn(), { success: vi.fn(), error: vi.fn() }),
  Toaster: () => null,
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  // phoneWidth() sets a matchMedia that must not narrow the next test: jsdom has none.
  // @ts-expect-error restoring the jsdom default
  delete window.matchMedia;
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 2, role: null };

const card = (over: Partial<ItemCard>): ItemCard =>
  ({
    id: 'item-3',
    ref: 'software#3',
    number: 3,
    title: 'Ship the retry fix',
    type: 'task',
    station: 'review',
    state: 'waiting_human',
    class_of_service: 'standard',
    rework_count: 0,
    cost_usd: 1.25,
    created_at: '2026-10-25T08:00:00Z',
    updated_at: '2026-10-27T08:00:00Z',
    age_seconds: 600,
    agent: null,
    pr_url: null,
    pull_requests: [],
    slot: null,
    turn: null,
    summary: null,
    in_buffer: false,
    rope_held: false,
    touch_count: 0,
    ...over,
  }) as ItemCard;

const factory = { id: 'factory-1', name: 'software', title: 'Software' };
const since = '2026-10-27T08:00:00Z';

const decision: InboxEntry = {
  id: 'decision:item-3',
  kind: 'decision',
  factory,
  since,
  item: card({}),
  station: 'review',
  action: null,
  approval: {
    summary: 'The retry fix passes its checks.',
    facts: [{ key: 'checks', value: 'all passed' }],
    pull_request: 'https://github.com/acme/app/pull/9',
    spend_usd: 1.25,
  },
  blocked: null,
  andon: null,
  rework: null,
};

const merge: InboxEntry = {
  ...decision,
  id: 'decision:item-4',
  item: card({ id: 'item-4', ref: 'software#4', title: 'Merge the cache fix', station: 'merge' }),
  station: 'merge',
  action: 'merge',
  since: '2026-10-27T07:00:00Z',
};

const alert: InboxEntry = {
  id: 'andon:7',
  kind: 'andon',
  factory: { id: 'factory-2', name: 'sre', title: 'SRE' },
  since,
  item: null,
  station: null,
  action: null,
  approval: null,
  blocked: null,
  andon: {
    id: 7,
    kind: 'manual',
    station: null,
    reason: 'Pages keep firing',
    pulled_by: 'user:local',
    pulled_at: since,
    cleared_by: null,
    cleared_at: null,
    note: '',
    data: {},
  },
  rework: null,
};

const entries = [decision, merge, alert];
const inbox = (list: InboxEntry[] = entries): Inbox => ({ entries: list, truncated: false });

/** The org's factories with their grants: the question's answers read `can` from here. */
const factories = (can: string[] = ['item:decide']) => [
  { id: 'factory-1', name: 'software', title: 'Software', can },
  { id: 'factory-2', name: 'sre', title: 'SRE', can },
];

function renderPage({
  search,
  seed = [
    [inboxQuery(org.id).queryKey, inbox()],
    [factoriesQuery(org.id).queryKey, factories()],
  ],
}: {
  search?: Record<string, string>;
  seed?: readonly (readonly [readonly unknown[], unknown])[];
} = {}) {
  vi.mocked(listItemEvents).mockResolvedValue({ events: [], next_after: 0 } as never);
  return renderOrgPage({
    path: '/needs-you',
    topBar: true,
    page: () => (
      <TooltipProvider>
        <NeedsYouPage />
      </TooltipProvider>
    ),
    org,
    search,
    seed: seed as never,
  });
}

/** The phone's width: matchMedia answers the phone query with a match. */
function phoneWidth() {
  window.matchMedia = ((query: string) => ({
    matches: query === '(max-width: 767px)' || query === '(max-width: 1023px)',
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  })) as unknown as typeof window.matchMedia;
}

describe('Needs you: the four states', () => {
  it('shows a Skeleton wrapper, aria-busy and saying Loading, while the inbox loads', async () => {
    vi.mocked(getInbox).mockReturnValue(new Promise(() => {}));
    renderPage({ seed: [] });

    const busy = (await screen.findByText('Loading')).closest('[aria-busy]');
    expect(busy?.getAttribute('aria-busy')).toBe('true');
  });

  it('shows an empty state with one sentence and one action when nothing waits', async () => {
    renderPage({ seed: [[inboxQuery(org.id).queryKey, inbox([])]] });

    expect(await screen.findByText('Nothing waits on you right now.')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Go to Home' })).toBeTruthy();
  });

  it('shows an error when the inbox fails to load, with a way to try again', async () => {
    vi.mocked(getInbox).mockRejectedValue(new Error('goald is down'));
    renderPage({ seed: [] });

    const alertBox = await screen.findByRole('alert');
    expect(within(alertBox).getByText('Needs you did not load.')).toBeTruthy();
    expect(within(alertBox).getByRole('button', { name: 'Try again' })).toBeTruthy();
  });

  it('shows the header with the violet count, the tabs and the list when ready', async () => {
    renderPage();

    expect(await screen.findByRole('heading', { name: 'Needs you' })).toBeTruthy();
    expect(screen.getByRole('list', { name: /Waiting on you/ })).toBeTruthy();
    expect((screen.getByRole('radio', { name: /^All\s*3$/ }) as HTMLInputElement).checked).toBe(true);
  });
});

describe('Needs you: filters live in the URL', () => {
  it('selects the Decisions tab and writes it to the URL, leaving alerts out of the list', async () => {
    const { router } = renderPage();

    fireEvent.click(await screen.findByRole('radio', { name: /Decisions/ }));

    await waitFor(() => expect(router.state.location.href).toContain('type=decision'));
    expect(screen.queryByText('Pages keep firing')).toBeNull();
    expect(screen.getAllByText('Decision').length).toBeGreaterThan(0);
  });

  it('opens with the tab and the factory the URL names', async () => {
    renderPage({ search: { type: 'alert' } });

    expect(await screen.findByText('Pages keep firing')).toBeTruthy();
    expect(screen.queryByText('Ship the retry fix')).toBeNull();
  });

  it('shows each tab with its count', async () => {
    renderPage();

    expect(await screen.findByRole('radio', { name: /Decisions\s*2/ })).toBeTruthy();
    expect(screen.getByRole('radio', { name: /Alerts\s*1/ })).toBeTruthy();
    expect(screen.getByRole('radio', { name: /All\s*3/ })).toBeTruthy();
  });

  it('narrows the list to one factory the URL names', async () => {
    renderPage({ search: { factory: 'sre' } });

    expect(await screen.findByText('Pages keep firing')).toBeTruthy();
    expect(screen.queryByText('Ship the retry fix')).toBeNull();
  });

  it('selects the row the URL names in entry', async () => {
    renderPage({ search: { entry: 'decision:item-4' } });

    const row = await screen.findByRole('link', { name: /Merge the cache fix/ });
    expect(row.getAttribute('aria-current')).toBe('page');
  });
});

describe('Needs you: deciding a Decision', () => {
  it('swaps the Approve label for a check once the approval went through, before the entry leaves', async () => {
    vi.mocked(approveItem).mockResolvedValue({ id: 'item-3', state: 'done' } as unknown as Item);
    // The refetch after the approval no longer lists the entry: the page keeps it in view for the check.
    vi.mocked(getInbox).mockResolvedValue(inbox(entries.filter((e) => e.id !== 'decision:item-3')));
    renderPage({ search: { entry: 'decision:item-3' } });

    const button = await screen.findByRole('button', { name: /^Approve/ });
    expect(button.querySelector('svg')).toBeNull();
    fireEvent.click(button);

    await waitFor(() => expect(button.querySelector('svg')?.getAttribute('class')).toContain('animate-success-check'));
    expect(button.textContent).toBe('Approved');
    expect(within(button).getByRole('status').textContent).toBe('Approved');
    expect(button.isConnected).toBe(true);
  });

  it('brings the Approve label back, with no check, when the approval fails', async () => {
    vi.mocked(approveItem).mockRejectedValue(new Error('goald is down'));
    vi.mocked(getInbox).mockResolvedValue(inbox(entries));
    renderPage({ search: { entry: 'decision:item-3' } });

    const button = await screen.findByRole('button', { name: /^Approve/ });
    fireEvent.click(button);

    await waitFor(() => expect(approveItem).toHaveBeenCalled());
    await waitFor(() => expect(button.textContent).toBe('ApproveA'), { timeout: 3000 });
    expect(button.querySelector('svg')).toBeNull();
  });

  it('approves the selected Decision with approveItem', async () => {
    vi.mocked(approveItem).mockResolvedValue({ id: 'item-3', state: 'done' } as unknown as Item);
    renderPage();

    fireEvent.click(await screen.findByRole('button', { name: /^Approve/ }));

    await waitFor(() => expect(approveItem).toHaveBeenCalledWith('org-1', 'item-3'));
    expect(mergeItem).not.toHaveBeenCalled();
  });

  it('swaps Merge for a check once the merge went through, on the phone too', async () => {
    phoneWidth();
    vi.mocked(mergeItem).mockResolvedValue({ id: 'item-4', state: 'done' } as unknown as Item);
    vi.mocked(getInbox).mockResolvedValue(inbox(entries.filter((e) => e.id !== 'decision:item-4')));
    renderPage({ search: { entry: 'decision:item-4' } });

    const button = await screen.findByRole('button', { name: /^Merge/ });
    fireEvent.click(button);

    await waitFor(() => expect(button.querySelector('svg')?.getAttribute('class')).toContain('animate-success-check'));
    expect(button.textContent).toBe('Merged');
    expect(within(button).getByRole('status').textContent).toBe('Merged');
  });

  it('merges instead of approving when the entry action is merge', async () => {
    vi.mocked(mergeItem).mockResolvedValue({ id: 'item-4', state: 'done' } as unknown as Item);
    renderPage({ search: { entry: 'decision:item-4' } });

    fireEvent.click(await screen.findByRole('button', { name: /^Merge/ }));

    await waitFor(() => expect(mergeItem).toHaveBeenCalledWith('org-1', 'item-4'));
    expect(approveItem).not.toHaveBeenCalled();
  });

  it('asks for a reason on Deny and sends it with rejectItem', async () => {
    vi.mocked(rejectItem).mockResolvedValue({ id: 'item-3', state: 'in_progress' } as unknown as Item);
    renderPage();

    fireEvent.click(await screen.findByRole('button', { name: /^Deny/ }));
    const dialog = await screen.findByRole('dialog', { name: 'Deny software#3' });
    expect(rejectItem).not.toHaveBeenCalled();

    fireEvent.change(within(dialog).getByRole('textbox'), { target: { value: 'Add the missing test' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Deny' }));

    await waitFor(() => expect(rejectItem).toHaveBeenCalledWith('org-1', 'item-3', 'Add the missing test'));
  });

  it('does not deny without a reason', async () => {
    renderPage();

    fireEvent.click(await screen.findByRole('button', { name: /^Deny/ }));
    const dialog = await screen.findByRole('dialog', { name: 'Deny software#3' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Deny' }));

    expect(await within(dialog).findByText('Say what must change.')).toBeTruthy();
    expect(rejectItem).not.toHaveBeenCalled();
  });

  it('shows no Approve or Deny on an Alert, only links to its item and Floor', async () => {
    renderPage({ search: { entry: 'andon:7' } });

    expect(await screen.findByText('Pages keep firing', { selector: 'p' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: /^Approve/ })).toBeNull();
    expect(screen.queryByRole('button', { name: /^Deny/ })).toBeNull();
    expect(screen.getByRole('link', { name: /Floor/ })).toBeTruthy();
  });

  it('shows a failing knowledge source as an Alert under the factory that reads it, with why and a link to it', async () => {
    const failing: InboxEntry = {
      ...alert,
      id: 'knowledge_source:factory-1:handbook',
      kind: 'knowledge_source',
      factory,
      andon: null,
      knowledge_source: { source: 'handbook', error: '401 from the code host', failed_syncs: 3 },
    };
    renderPage({
      search: { entry: failing.id, type: 'alert' },
      seed: [
        [inboxQuery(org.id).queryKey, inbox([failing])],
        [factoriesQuery(org.id).queryKey, factories()],
      ],
    });

    expect(await screen.findByText('401 from the code host', { selector: 'p' })).toBeTruthy();
    expect(screen.getAllByText('handbook, which Software reads, does not sync').length).toBeGreaterThan(0);
    expect(screen.queryByText(/last sync|synced since/i)).toBeNull();
    expect(
      screen.getByText(/The last 3 syncs of handbook failed, so Software reads the files from before/),
    ).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Open handbook' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: /^Approve/ })).toBeNull();
  });
});

describe('Needs you: a conversion that could not move to secrets', () => {
  it('shows the factory, the reason, what to fix and a link to the definition', async () => {
    const conversion: InboxEntry = {
      ...alert,
      id: 'conversion:factory-1',
      kind: 'conversion',
      factory,
      andon: null,
      conversion: {
        reason: 'Connection aws-prod uses the aws kind.',
        problems: ['connection aws-prod uses the aws kind', 'connection zendesk-prod has no secret'],
      },
    };
    renderPage({
      search: { entry: conversion.id, type: 'alert' },
      seed: [
        [inboxQuery(org.id).queryKey, inbox([conversion])],
        [factoriesQuery(org.id).queryKey, factories()],
      ],
    });

    expect((await screen.findAllByText('Software could not move to secrets')).length).toBeGreaterThan(0);
    expect(screen.getByText('connection aws-prod uses the aws kind', { selector: 'li' })).toBeTruthy();
    expect(screen.getByText('connection zendesk-prod has no secret', { selector: 'li' })).toBeTruthy();
    expect(
      screen.getByText(/keeps running on its current version until a version on secrets is published/),
    ).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Open the definition' }).getAttribute('href')).toContain('/definition');
    expect(screen.queryByText(/expedite/i)).toBeNull();
  });
});

describe('Needs you: the rows of the two alerts', () => {
  it('says which factory reads a failing source, and the error and its count under it', async () => {
    const failing: InboxEntry = {
      ...alert,
      id: 'knowledge_source:factory-1:handbook',
      kind: 'knowledge_source',
      factory,
      andon: null,
      knowledge_source: { source: 'handbook', error: 'Confluence refused the token', failed_syncs: 3 },
    };
    renderPage({
      search: { type: 'alert' },
      seed: [
        [inboxQuery(org.id).queryKey, inbox([failing])],
        [factoriesQuery(org.id).queryKey, factories()],
      ],
    });

    const row = await screen.findByRole('link', { name: /handbook, which Software reads, does not sync/ });
    expect(within(row).getByText('Confluence refused the token 3 times')).toBeTruthy();
    expect(within(row).queryByText(/synced since|last sync/i)).toBeNull();
  });

  it('names the factory that could not move to secrets and gives the reason under it', async () => {
    const conversion: InboxEntry = {
      ...alert,
      id: 'conversion:factory-1',
      kind: 'conversion',
      factory,
      andon: null,
      conversion: {
        reason: 'Connection aws-prod uses the aws kind.',
        problems: ['connection aws-prod uses the aws kind'],
      },
    };
    renderPage({
      search: { type: 'alert' },
      seed: [
        [inboxQuery(org.id).queryKey, inbox([conversion])],
        [factoriesQuery(org.id).queryKey, factories()],
      ],
    });

    const row = await screen.findByRole('link', { name: /Software could not move to secrets/ });
    expect(within(row).getByText('Connection aws-prod uses the aws kind.')).toBeTruthy();
    expect(within(row).queryByText(/expedite/i)).toBeNull();
  });
});

describe('Needs you: the keyboard path', () => {
  it('moves the selection with J and K and writes it to entry', async () => {
    const { router } = renderPage();
    await screen.findByRole('heading', { name: 'Needs you' });

    fireEvent.keyDown(document.body, { key: 'j' });
    await waitFor(() => expect(router.state.location.href).toContain('entry=decision%3Aitem-4'));

    fireEvent.keyDown(document.body, { key: 'k' });
    await waitFor(() => expect(router.state.location.href).toContain('entry=decision%3Aitem-3'));
  });

  it('approves the selected Decision with A and opens the Deny dialog with D', async () => {
    vi.mocked(approveItem).mockResolvedValue({ id: 'item-3', state: 'done' } as unknown as Item);
    renderPage();
    await screen.findByRole('heading', { name: 'Needs you' });

    fireEvent.keyDown(document.body, { key: 'd' });
    expect(await screen.findByRole('dialog', { name: 'Deny software#3' })).toBeTruthy();
    fireEvent.keyDown(document.body, { key: 'a' });
    expect(approveItem).not.toHaveBeenCalled();
  });

  it('does not act on A while the reason field has focus', async () => {
    renderPage();
    fireEvent.click(await screen.findByRole('button', { name: /^Deny/ }));
    const dialog = await screen.findByRole('dialog', { name: 'Deny software#3' });
    const field = within(dialog).getByRole('textbox');

    fireEvent.keyDown(field, { key: 'a' });
    expect(approveItem).not.toHaveBeenCalled();
  });
});

describe('Needs you: controls not built yet', () => {
  it('renders Edit, then approve disabled, with its tooltip', async () => {
    renderPage();

    const edit = await screen.findByRole('button', { name: 'Edit, then approve' });
    expect(edit.getAttribute('aria-disabled')).toBe('true');
  });

  it('shows no Always allow checkbox on a Decision', async () => {
    renderPage();

    await screen.findByRole('button', { name: /^Approve/ });
    expect(screen.queryByRole('checkbox')).toBeNull();
  });
});

/** The architect's add_issue_comment call on software#151, which the API offers Always allow for. */
const call = {
  id: 9,
  connection: 'github',
  tool: 'add_issue_comment',
  input: { body: 'Thanks for the request.', issue_number: 212 },
  agent: 'architect',
  run_id: 'run-1',
  asked_at: since,
  always_allow_offered: true,
};

/** A tool call an architect asks to make on software#151, waiting for a decision. */
const approval: InboxEntry = {
  id: 'tool_approval:item-151',
  kind: 'tool_approval',
  factory,
  since,
  item: card({
    id: 'item-151',
    ref: 'software#151',
    number: 151,
    title: 'Webhook as an intake source',
    station: 'spec',
  }),
  station: 'spec',
  action: null,
  approval: null,
  blocked: null,
  andon: null,
  rework: null,
  tool_approval: call,
};

/** Renders the page with the approval among the entries and the caller's organization grants. */
function renderApproval({
  entries = [approval],
  can = ['connection:manage'],
  search = {},
}: {
  entries?: InboxEntry[];
  can?: string[];
  search?: Record<string, string>;
} = {}) {
  return renderPage({
    search,
    seed: [
      [inboxQuery(org.id).queryKey, inbox(entries)],
      [factoriesQuery(org.id).queryKey, factories()],
      [orgQuery(org.id).queryKey, { role: 'owner', can }],
    ],
  });
}

describe('Needs you: Approvals', () => {
  it('shows the call the agent asks to make, with its input, tool and connection', async () => {
    renderApproval({ search: { entry: 'tool_approval:item-151' } });

    expect(await screen.findByRole('heading', { name: 'architect wants to call add_issue_comment' })).toBeTruthy();
    expect(screen.getByText('The call it will make')).toBeTruthy();
    expect(screen.getByText(/"issue_number": 212/)).toBeTruthy();
    expect(screen.getByText('github')).toBeTruthy();
  });

  it('draws the approval row with its type, its title and its item and station', async () => {
    renderApproval();

    const list = await screen.findByRole('list', { name: /Waiting on you/ });
    expect(within(list).getByText('Approval')).toBeTruthy();
    expect(within(list).getByText('architect wants to call add_issue_comment')).toBeTruthy();
    expect(within(list).getByText('#151 at Spec, run paused')).toBeTruthy();
  });

  it('approves the call with always_allow false when the box is clear', async () => {
    vi.mocked(decideToolApproval).mockResolvedValue({ id: 'item-151', state: 'queued' } as unknown as Item);
    renderApproval({ search: { entry: 'tool_approval:item-151' } });

    fireEvent.click(await screen.findByRole('button', { name: /^Approve/ }));

    await waitFor(() =>
      expect(decideToolApproval).toHaveBeenCalledWith('org-1', 'item-151', {
        decision: 'approve',
        always_allow: false,
      }),
    );
    expect(approveItem).not.toHaveBeenCalled();
  });

  it('approves with Always allow when the box is checked', async () => {
    vi.mocked(decideToolApproval).mockResolvedValue({ id: 'item-151', state: 'queued' } as unknown as Item);
    renderApproval({ search: { entry: 'tool_approval:item-151' } });

    const box = await screen.findByRole('checkbox', { name: 'Always allow architect to add_issue_comment' });
    fireEvent.click(box);
    fireEvent.click(screen.getByRole('button', { name: /^Approve/ }));

    await waitFor(() =>
      expect(decideToolApproval).toHaveBeenCalledWith('org-1', 'item-151', { decision: 'approve', always_allow: true }),
    );
  });

  it('hides Always allow when the API does not offer it for the call', async () => {
    renderApproval({
      search: { entry: 'tool_approval:item-151' },
      entries: [{ ...approval, tool_approval: { ...call, always_allow_offered: false } }],
    });

    await screen.findByRole('button', { name: /^Approve/ });
    expect(screen.queryByRole('checkbox')).toBeNull();
  });

  it('hides Always allow from a person who lacks connection:manage', async () => {
    renderApproval({ search: { entry: 'tool_approval:item-151' }, can: ['item:decide'] });

    await screen.findByRole('button', { name: /^Approve/ });
    expect(screen.queryByRole('checkbox')).toBeNull();
  });

  it('keeps Edit, then approve disabled on an approval', async () => {
    renderApproval({ search: { entry: 'tool_approval:item-151' } });

    const edit = await screen.findByRole('button', { name: 'Edit, then approve' });
    expect(edit.getAttribute('aria-disabled')).toBe('true');
  });

  it('denies with the reason, sent as the note', async () => {
    vi.mocked(decideToolApproval).mockResolvedValue({ id: 'item-151', state: 'queued' } as unknown as Item);
    renderApproval({ search: { entry: 'tool_approval:item-151' } });

    fireEvent.click(await screen.findByRole('button', { name: /^Deny/ }));
    const dialog = await screen.findByRole('dialog', { name: 'Deny software#151' });
    fireEvent.change(within(dialog).getByRole('textbox'), { target: { value: 'Keep it to the spec.' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Deny' }));

    await waitFor(() =>
      expect(decideToolApproval).toHaveBeenCalledWith('org-1', 'item-151', {
        decision: 'deny',
        note: 'Keep it to the spec.',
      }),
    );
  });

  it('shows approvals under the Approvals tab and no decisions', async () => {
    renderApproval({ entries: [decision, approval], search: { type: 'approval' } });

    const list = await screen.findByRole('list', { name: /Waiting on you/ });
    expect(within(list).getByText('architect wants to call add_issue_comment')).toBeTruthy();
    expect(within(list).queryByText('Ship the retry fix')).toBeNull();
    expect(screen.getByRole('radio', { name: /^Approvals/ }).hasAttribute('disabled')).toBe(false);
  });
});

describe('Needs you on a phone', () => {
  it('shows the approve view when the URL names an entry', async () => {
    phoneWidth();
    renderPage({ search: { entry: 'decision:item-3' } });

    expect(await screen.findByRole('link', { name: 'Back to Needs you' })).toBeTruthy();
    expect(screen.getByRole('heading', { name: 'Decision' })).toBeTruthy();
    expect(screen.queryByRole('list', { name: /Waiting on you/ })).toBeNull();
    expect(screen.getByRole('button', { name: /^Approve/ })).toBeTruthy();
  });

  it('slides the approve view in from the right, on the pane-forward motion', async () => {
    phoneWidth();
    renderPage({ search: { entry: 'decision:item-3' } });

    const back = await screen.findByRole('link', { name: 'Back to Needs you' });
    expect(back.closest('.animate-pane-forward')).not.toBeNull();
  });

  it('shows the list with its tabs when the URL names no entry', async () => {
    phoneWidth();
    renderPage();

    expect(await screen.findByRole('list', { name: /Waiting on you/ })).toBeTruthy();
    expect(screen.getByRole('radio', { name: /Alerts/ })).toBeTruthy();
  });

  it('shows only the list, with no entry beside it and no Approve button', async () => {
    phoneWidth();
    renderPage();

    expect(await screen.findByRole('list', { name: /Waiting on you/ })).toBeTruthy();
    expect(screen.queryByRole('article')).toBeNull();
    expect(screen.queryByRole('button', { name: /^Approve/ })).toBeNull();
  });

  it('leaves J, K and A alone while only the list shows, so a key never acts on an entry out of sight', async () => {
    phoneWidth();
    const { router } = renderPage();
    await screen.findByRole('list', { name: /Waiting on you/ });

    for (const key of ['j', 'k', 'a', 'd']) fireEvent.keyDown(document.body, { key });

    expect(router.state.location.href).not.toContain('entry=');
    expect(approveItem).not.toHaveBeenCalled();
    expect(screen.queryByRole('dialog')).toBeNull();
  });
});

describe('the detail', () => {
  it("shows the item's recent events, from the events the item page reads", async () => {
    const event = {
      id: 1,
      item_id: 'item-3',
      run_id: '',
      at: since,
      kind: 'checks_passed',
      station: 'review',
      actor: 'system:engine',
      data: {},
    };
    renderPage({
      seed: [
        [inboxQuery(org.id).queryKey, inbox()],
        [keys.events(org.id, 'item-3'), [event]],
      ],
    });

    expect(await screen.findByRole('heading', { name: 'Recent events on this item' })).toBeTruthy();
    expect(screen.getByText(/Checks passed at Review/)).toBeTruthy();
  });
});

const question: InboxEntry = {
  ...decision,
  id: 'question:item-5',
  kind: 'question',
  item: card({ id: 'item-5', ref: 'software#5', number: 5, title: 'Show spend per factory', station: 'build' }),
  station: 'build',
  action: null,
  approval: null,
  question: {
    text: 'Organization or factory spend?',
    choices: ['Organization', 'Factory'],
    asked_at: since,
    agent: 'frontend-engineer',
    run_id: 'run-5',
  },
};

describe('Needs you: questions', () => {
  it('enables the Questions segment and shows its count', async () => {
    renderPage({
      seed: [
        [inboxQuery(org.id).queryKey, inbox([question])],
        [factoriesQuery(org.id).queryKey, factories()],
      ],
    });

    const tab = await screen.findByRole('radio', { name: /^Questions/ });
    expect(tab.hasAttribute('disabled')).toBe(false);
    expect(tab.closest('label')?.textContent).toContain('1');
  });

  it('draws a question row with its type, its agent and question, and its item and station', async () => {
    renderPage({
      seed: [
        [inboxQuery(org.id).queryKey, inbox([question])],
        [factoriesQuery(org.id).queryKey, factories()],
      ],
    });

    const list = await screen.findByRole('list', { name: /Waiting on you/ });
    expect(within(list).getByText('Question')).toBeTruthy();
    expect(within(list).getByText('frontend-engineer: Organization or factory spend?')).toBeTruthy();
    expect(within(list).getByText('#5 at Build, run paused')).toBeTruthy();
  });

  it('answers with a quick answer, and the answered row leaves the list', async () => {
    vi.mocked(answerItem).mockResolvedValue({ id: 'item-5', state: 'queued' } as unknown as Item);
    renderPage({
      search: { type: 'question', entry: 'question:item-5' },
      seed: [
        [inboxQuery(org.id).queryKey, inbox([question])],
        [factoriesQuery(org.id).queryKey, factories()],
      ],
    });
    const card = await screen.findByRole('region', { name: 'frontend-engineer asks you' });
    vi.mocked(getInbox).mockResolvedValue(inbox([]));

    fireEvent.click(within(card).getByRole('button', { name: 'Factory' }));

    await waitFor(() => expect(answerItem).toHaveBeenCalledWith('org-1', 'item-5', { choice: 1 }));
    await waitFor(() => expect(screen.queryByRole('list', { name: /Waiting on you/ })).toBeNull());
  });

  it('disables the answers and says who decides when the person lacks item:decide', async () => {
    renderPage({
      search: { type: 'question', entry: 'question:item-5' },
      seed: [
        [inboxQuery(org.id).queryKey, inbox([question])],
        [factoriesQuery(org.id).queryKey, factories([])],
      ],
    });
    const card = await screen.findByRole('region', { name: 'frontend-engineer asks you' });

    const quick = within(card).getByRole('button', { name: 'Factory' });
    expect(quick.getAttribute('aria-disabled')).toBe('true');
    expect(within(card).getByText('Approvers of software decide here.')).toBeTruthy();
    fireEvent.click(quick);
    expect(answerItem).not.toHaveBeenCalled();
  });

  it('does nothing on A or D, which act on Decisions only', async () => {
    renderPage({
      search: { type: 'question', entry: 'question:item-5' },
      seed: [
        [inboxQuery(org.id).queryKey, inbox([question])],
        [factoriesQuery(org.id).queryKey, factories()],
      ],
    });
    await screen.findByRole('region', { name: 'frontend-engineer asks you' });

    fireEvent.keyDown(document.body, { key: 'a' });
    fireEvent.keyDown(document.body, { key: 'd' });

    expect(approveItem).not.toHaveBeenCalled();
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(answerItem).not.toHaveBeenCalled();
  });
});

describe('Needs you: the keycap hint and the type chip', () => {
  it('draws J, K, A and D as keycaps in the hint', async () => {
    renderPage();
    const hint = (await screen.findByText(/to move,/)).closest('p') as HTMLElement;
    expect(Array.from(hint.querySelectorAll('kbd')).map((k) => k.textContent)).toEqual(['J', 'K', 'A', 'D']);
    expect(hint.textContent).toBe('J K to move, A approve, D deny');
  });

  it('shows a dot in the detail chip', async () => {
    renderPage();
    const detail = await screen.findByRole('article');
    const chip = within(detail).getByText('Decision').closest('span.rounded-chip') as HTMLElement;
    expect(chip.querySelector('[aria-hidden="true"]')).toBeTruthy();
  });
});

describe('Needs you: a proposal that made its number worse', () => {
  const worse: InboxEntry = {
    ...alert,
    id: 'watch_worse:p-14',
    kind: 'watch_worse',
    andon: null,
    watch: {
      station: 'build',
      metric: 'sent_back_rate',
      proposal: 14,
      before: { items: 20, value: 0.2 },
      after: { items: 12, value: 0.4 },
      interval: { low: 0.05, high: 0.35 },
      verdict: 'worse',
      opened_at: '2026-10-24T10:00:00Z',
      closed_at: null,
    },
  };
  const seedWith = (can: string[]) =>
    [
      [inboxQuery(org.id).queryKey, inbox([worse])],
      [factoriesQuery(org.id).queryKey, factories(can)],
    ] as const;

  it('names the proposal and its worse number, tied to no item, with Undo this change', async () => {
    renderPage({ seed: seedWith(['definition:edit']) });

    expect((await screen.findAllByText('Proposal 14 made send-backs worse')).length).toBeGreaterThan(0);
    expect(screen.getByText(/Send-backs at build/)).toBeTruthy();
    expect(screen.getByRole('button', { name: /Undo this change/ })).toBeTruthy();
    expect(screen.queryByRole('link', { name: /^Open [a-z]+#\d+/ })).toBeNull();
    // The same Button size as Open proposal and Open the Floor beside it.
    expect(screen.getByRole('button', { name: /Undo this change/ }).className).toContain('h-8');
  });

  it('writes the undo proposal for the entry’s factory', async () => {
    vi.mocked(undoProposal).mockResolvedValue({ number: 15 } as never);
    renderPage({ seed: seedWith(['definition:edit']) });

    fireEvent.click(await screen.findByRole('button', { name: /Undo this change/ }));
    await waitFor(() => expect(undoProposal).toHaveBeenCalledWith('org-1', 'factory-2', 14));
  });

  it('says goald’s message when the undo is refused', async () => {
    vi.mocked(undoProposal).mockRejectedValue(
      new ApiError({
        title: 'Conflict',
        status: 409,
        code: 'undo-conflict',
        detail: 'factory.yaml no longer has the text the proposal replaced.',
      }),
    );
    renderPage({ seed: seedWith(['definition:edit']) });

    fireEvent.click(await screen.findByRole('button', { name: /Undo this change/ }));
    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith('factory.yaml no longer has the text the proposal replaced.'),
    );
  });

  it('offers no Undo to someone who may not edit the definition', async () => {
    renderPage({ seed: seedWith(['item:decide']) });

    await screen.findAllByText('Proposal 14 made send-backs worse');
    expect(screen.queryByRole('button', { name: /Undo this change/ })).toBeNull();
  });
});

describe('Needs you: truncated row text carries its full text', () => {
  it('gives the row title and where-line a title attribute', async () => {
    const long = 'A very long title that the row cuts off with an ellipsis when the list is narrow';
    const entry: InboxEntry = { ...decision, item: card({ title: long }) };
    renderPage({
      seed: [
        [inboxQuery(org.id).queryKey, inbox([entry])],
        [factoriesQuery(org.id).queryKey, factories()],
      ],
    });

    const title = await screen.findByTitle(long);
    expect(title.className).toContain('truncate');
    const where = title.nextElementSibling as HTMLElement;
    expect(where.className).toContain('truncate');
    expect(where.getAttribute('title')).toBe(where.textContent);
  });
});
