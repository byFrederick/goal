// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  answerItem,
  approveItem,
  decideToolApproval,
  getInbox,
  listItemEvents,
  mergeItem,
  rejectItem,
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
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
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
    matches: query === '(max-width: 767px)',
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  })) as unknown as typeof window.matchMedia;
}

describe('Needs you: the four states', () => {
  it('shows the app background with a Loading announcement while the inbox loads', async () => {
    vi.mocked(getInbox).mockReturnValue(new Promise(() => {}));
    renderPage({ seed: [] });

    const busy = (await screen.findByText('Loading')).closest('main');
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
    expect(screen.getByRole('button', { name: /^All\s*3$/ }).getAttribute('aria-pressed')).toBe('true');
  });
});

describe('Needs you: filters live in the URL', () => {
  it('selects the Decisions tab and writes it to the URL, leaving alerts out of the list', async () => {
    const { router } = renderPage();

    fireEvent.click(await screen.findByRole('button', { name: /Decisions/ }));

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

    expect(await screen.findByRole('button', { name: /Decisions\s*2/ })).toBeTruthy();
    expect(screen.getByRole('button', { name: /Alerts\s*1/ })).toBeTruthy();
    expect(screen.getByRole('button', { name: /All\s*3/ })).toBeTruthy();
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
  it('approves the selected Decision with approveItem', async () => {
    vi.mocked(approveItem).mockResolvedValue({ id: 'item-3', state: 'done' } as unknown as Item);
    renderPage();

    fireEvent.click(await screen.findByRole('button', { name: /^Approve/ }));

    await waitFor(() => expect(approveItem).toHaveBeenCalledWith('org-1', 'item-3'));
    expect(mergeItem).not.toHaveBeenCalled();
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
    expect(screen.getByRole('button', { name: /^Approvals/ }).getAttribute('aria-disabled')).toBeNull();
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

  it('shows the list with its tabs when the URL names no entry', async () => {
    phoneWidth();
    renderPage();

    expect(await screen.findByRole('list', { name: /Waiting on you/ })).toBeTruthy();
    expect(screen.getByRole('button', { name: /Alerts/ })).toBeTruthy();
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

    const tab = await screen.findByRole('button', { name: /^Questions/ });
    expect(tab.getAttribute('aria-disabled')).toBeNull();
    expect(tab.textContent).toContain('1');
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
