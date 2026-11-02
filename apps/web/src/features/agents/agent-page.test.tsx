// @vitest-environment jsdom
import type { Problem } from '@goal/api-client';
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError, startBakeOff } from '../../api/client';
import { createDraftRequest } from '../../api/definitions';
import {
  agentStatsQuery,
  builderMetaQuery,
  factoryQuery,
  versionFileQuery,
  versionModelQuery,
  versionQuery,
} from '../../api/queries';
import type { AgentStats, Factory, OrgSummary } from '../../api/types';
import { renderFactoryPage, type Seed } from '../../test/route';
import { TooltipProvider } from '../../ui/tooltip';
import { touchLink } from '../../ui/touch-target';
import { DraftSession } from '../factory-editor/draft/session';
import { AgentPage } from './agent-page';
import type { AgentView } from './views';

vi.mock('../../api/client', async (original) => ({
  ...(await original<typeof import('../../api/client')>()),
  startBakeOff: vi.fn(),
}));

vi.mock('../../api/definitions', async (original) => ({
  ...(await original<typeof import('../../api/definitions')>()),
  createDraftRequest: vi.fn(),
}));

afterEach(cleanup);
beforeEach(() => {
  vi.restoreAllMocks();
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: 'owner' };
const factory: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'goal',
  title: 'Goal',
  work_types: ['feature'],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'editor',
  can: ['item:create', 'factory:manage'],
};

const promptPath = 'agents/frontend-engineer.md';
const promptText = '# frontend-engineer\n\nBuild the change the spec describes in web/.';

const stats: AgentStats = {
  agent: 'frontend-engineer',
  from: '2026-10-20',
  to: '2026-10-26',
  model: 'claude-haiku-5-5',
  stations: [{ id: 'build', slots: 2, constraint: true }],
  tasks_finished: 41,
  sent_back: { count: 9, ratio: 0.22 },
  median_turns: 46,
  spend_per_task_usd: 1.9,
  task_time_p85_minutes: 38,
  spend_usd: 77.9,
  reasons: [
    { label: 'No test for a new empty or error state', count: 4 },
    { label: 'Raw colour instead of a design token', count: 2 },
    { label: 'Copy not in plain language', count: 2 },
    { label: 'Icon button without a label', count: 1 },
  ],
  denied_tool_calls: { count: 0, tools: [] },
};

// The version that runs: the agent's own model, turns and per-item limit, and the factory's defaults.
const model = {
  basics: {},
  line: {},
  connections: [],
  review: { budget: { per_item_usd: 12, max_turns_per_task: 150 } },
  agents: {
    default_model: 'claude-opus-5-5',
    skills: [],
    agents: [
      {
        name: 'frontend-engineer',
        description: 'The React UI in apps/web and the TypeScript packages, following the design system exactly.',
        prompt: { path: `./${promptPath}`, sha256: 'sha-prompt' },
        model: 'claude-haiku-5-5',
        max_turns: 250,
        per_item_usd: 20,
        skills: [],
        connections: ['github', 'context7'],
      },
    ],
  },
};

/** The query data the page reads, so its first render has it and no request leaves the test. */
function seeds({
  agentStats = stats,
  version = { model },
  reasons = true,
}: {
  agentStats?: AgentStats;
  version?: unknown;
  reasons?: boolean;
} = {}): Seed[] {
  const seed: Seed[] = [
    [
      agentStatsQuery(org.id, factory.id, 'frontend-engineer').queryKey,
      reasons ? agentStats : { ...agentStats, reasons: [] },
    ],
    [factoryQuery(org.id, factory.id).queryKey, factory],
    [builderMetaQuery(org.id).queryKey, { capabilities: { tool_approvals: false } }],
    [versionQuery(org.id, factory.id, 'current').queryKey, { files: [{ path: promptPath, sha256: 'sha-prompt' }] }],
    [versionFileQuery(org.id, factory.id, 'current', promptPath, 'sha-prompt').queryKey, { text: promptText }],
  ];
  // `version: false` leaves the version that runs unread, as the page sees it while it loads.
  if (version !== false) seed.push([versionModelQuery(org.id, factory.id, 'current').queryKey, version]);
  return seed;
}

/** Renders the page and waits for its first render: the router mounts the page after its match. */
async function renderPage(
  view: AgentView = 'overview',
  options: Parameters<typeof seeds>[0] & { can?: Factory['can'] } = {},
) {
  const { can, ...seedOptions } = options;
  const result = renderFactoryPage({
    page: () => (
      <TooltipProvider>
        <AgentPage agent="frontend-engineer" view={view} />
      </TooltipProvider>
    ),
    path: 'agents/$agent',
    location: 'agents/frontend-engineer',
    org,
    factory: can ? { ...factory, can } : factory,
    seed: seeds(seedOptions),
    search: view === 'overview' ? undefined : { view },
    topBar: true,
  });
  await screen.findByRole('heading', { level: 1, name: 'frontend-engineer' });
  return result;
}

describe('the Agent page, ready', () => {
  it('names the agent, says where it works and lists the views', async () => {
    await renderPage();
    expect(screen.getByRole('heading', { level: 1, name: 'frontend-engineer' })).toBeTruthy();
    expect(
      screen.getByText('The React UI in apps/web and the TypeScript packages, following the design system exactly.'),
    ).toBeTruthy();
    expect(screen.getByText('Build station')).toBeTruthy();

    const views = screen.getByRole('navigation', { name: 'Agent views' });
    expect(
      within(views)
        .getAllByRole('link')
        .map((l) => l.textContent),
    ).toEqual(['Overview', 'Prompt', 'Tools and access', 'Runs41 this week']);
    expect(within(views).getByRole('link', { name: 'Overview' }).getAttribute('aria-current')).toBe('page');
  });

  it('shows the last 7 days and the reasons review sent work back, by role', async () => {
    await renderPage();
    const measures = screen.getByRole('region', { name: 'Last 7 days' });
    expect(within(measures).getByText('$1.90')).toBeTruthy();
    expect(within(measures).getByText('22%')).toBeTruthy();

    const reasons = screen.getByRole('region', { name: 'Why review sent work back' });
    expect(within(reasons).getAllByRole('listitem')).toHaveLength(4);
    expect(within(reasons).getByText('No test for a new empty or error state')).toBeTruthy();
    expect(screen.queryByText(/copy \d/i)).toBeNull();
    expect(reasons.querySelectorAll('.bg-rework').length).toBeGreaterThan(0);
    expect(reasons.querySelector('[class*="repeating"], [class*="hatch"]')).toBeNull();
  });

  it('shows each reason in sentence case on one line, with its full text in a tooltip and a solid bar', async () => {
    const long = 'nothing was delivered. after a fresh `git fetch` the branch had no commits';
    await renderPage('overview', { agentStats: { ...stats, reasons: [{ label: long, count: 3 }] } });
    const reasons = screen.getByRole('region', { name: 'Why review sent work back' });
    const line = within(reasons).getByText(/^Nothing was delivered\./);
    expect(line.className).toContain('truncate');
    expect(line.getAttribute('title')).toBe(long);
    line.focus();
    expect((await screen.findAllByText(long)).length).toBeGreaterThan(0);
    expect(reasons.querySelector('.bg-rework')).not.toBeNull();
    expect(reasons.querySelector('[class*="hatch"], [style*="repeating"]')).toBeNull();
  });

  it('shows 7 skills and a + N more chip that reveals the rest', async () => {
    const user = userEvent.setup();
    const skills = Array.from({ length: 20 }, (_, i) => `skill-${i + 1}`);
    const agent = model.agents.agents[0];
    await renderPage('overview', {
      version: { model: { ...model, agents: { ...model.agents, agents: [{ ...agent, skills }] } } },
    });
    const settings = screen.getByRole('region', { name: 'Settings' });
    const shown = () =>
      within(settings)
        .getAllByText(/^skill-\d+$/)
        .filter((el) => !el.closest('[inert]'));
    expect(shown()).toHaveLength(7);
    const more = within(settings).getByRole('button', { name: '+ 13 more' });
    expect(more.getAttribute('aria-expanded')).toBe('false');
    expect(more.className).toContain('max-md:min-h-11');
    expect(more.className).not.toContain('before:');
    await user.click(more);
    expect(shown()).toHaveLength(20);
    expect(more.getAttribute('aria-expanded')).toBe('true');
    expect(document.getElementById(more.getAttribute('aria-controls') ?? '')?.getAttribute('data-state')).toBe('open');
  });

  it('shows no more chip for 7 skills or fewer', async () => {
    const skills = Array.from({ length: 7 }, (_, i) => `skill-${i + 1}`);
    const agent = model.agents.agents[0];
    await renderPage('overview', {
      version: { model: { ...model, agents: { ...model.agents, agents: [{ ...agent, skills }] } } },
    });
    const settings = screen.getByRole('region', { name: 'Settings' });
    expect(within(settings).queryByRole('button', { name: /more/ })).toBeNull();
  });

  it('settles each setting as its own value or the factory default', async () => {
    await renderPage();
    const settings = screen.getByRole('region', { name: 'Settings' });
    expect(within(settings).getByText('claude-haiku-5-5')).toBeTruthy();
    expect(within(settings).getByText('250')).toBeTruthy();
    expect(within(settings).getByText('Default 150')).toBeTruthy();
    expect(within(settings).getByText('$20.00')).toBeTruthy();
    expect(within(settings).getByText('Default $12.00')).toBeTruthy();
    expect(within(settings).getAllByText('Own value')).toHaveLength(3);
  });

  it('shows the start of the prompt and links to all of it on the Prompt view', async () => {
    await renderPage();
    expect(screen.getByText(/Build the change the spec describes/)).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Show the full prompt' }).getAttribute('href')).toContain('view=prompt');
  });

  it('shows the whole prompt on the Prompt view', async () => {
    await renderPage('prompt');
    expect(screen.getByText(/Build the change the spec describes/)).toBeTruthy();
    expect(screen.getByText(promptPath)).toBeTruthy();
  });

  it('shows the skills, tool policy and the blocked calls on Tools and access', async () => {
    await renderPage('tools', {
      agentStats: { ...stats, denied_tool_calls: { count: 2, tools: [{ tool: 'aws ssm put-parameter', count: 2 }] } },
    });
    expect(screen.getByRole('region', { name: 'Skills' })).toBeTruthy();
    expect(screen.getByRole('region', { name: 'Tool policy' })).toBeTruthy();
    expect(screen.getByText('Blocked this week: 2 calls, both aws ssm put-parameter.')).toBeTruthy();
  });
});

describe('the top bar', () => {
  const edit: Factory['can'] = ['item:create', 'factory:manage', 'definition:edit'];

  async function renderWithBar(can?: Factory['can']) {
    const result = renderFactoryPage({
      page: () => (
        <TooltipProvider>
          <AgentPage agent="frontend-engineer" view="overview" />
        </TooltipProvider>
      ),
      path: 'agents/$agent',
      location: 'agents/frontend-engineer',
      org,
      factory: can ? { ...factory, can } : factory,
      seed: seeds(),
      topBar: true,
    });
    await screen.findByRole('heading', { level: 1, name: 'frontend-engineer' });
    return result;
  }

  it('ends the crumb with the agent and draws no breadcrumb of its own', async () => {
    await renderWithBar();
    const crumbs = screen.getAllByRole('navigation', { name: 'Breadcrumb' });
    expect(crumbs).toHaveLength(1);
    expect(crumbs[0]?.textContent).toContain('Agents');
    expect(crumbs[0]?.textContent).toContain('frontend-engineer');
    expect(
      screen.getByRole('heading', { level: 1 }).closest('div')?.querySelector('nav[aria-label="Breadcrumb"]'),
    ).toBeNull();
  });

  it('holds the actions in order, Save changes last, and not in the page body', async () => {
    await renderWithBar(edit);
    const names = ['Bake off this agent', 'Open prompt file', 'Save changes'];
    const found = names.map((n) => screen.getByRole(n === 'Open prompt file' ? 'link' : 'button', { name: n }));
    for (let i = 1; i < found.length; i++) {
      const prev = found[i - 1] as HTMLElement;
      expect(prev.compareDocumentPosition(found[i] as HTMLElement) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    }
    expect(
      screen
        .getByRole('heading', { level: 1 })
        .closest('header')
        ?.parentElement?.contains(found[0] as Node),
    ).toBe(false);
  });

  it('sends the waiting edit with the session flush, once', async () => {
    const flush = vi.spyOn(DraftSession.prototype, 'flush').mockResolvedValue();
    await renderWithBar(edit);
    const settings = within(screen.getByRole('region', { name: 'Settings' }));
    fireEvent.click(settings.getByRole('button', { name: 'Reset to default, claude-opus-5-5' }));
    expect(flush).toHaveBeenCalledTimes(1);
    const save = screen.getByRole('button', { name: 'Save changes' });
    await waitFor(() => expect(save.getAttribute('aria-disabled')).toBeNull());
    fireEvent.click(save);
    expect(flush).toHaveBeenCalledTimes(2);
  });

  it('keeps Save changes off while nothing waits, and Not available without definition:edit', async () => {
    await renderWithBar(edit);
    expect(screen.getByRole('button', { name: 'Save changes' }).getAttribute('aria-disabled')).toBe('true');
    cleanup();
    await renderWithBar();
    const save = screen.getByRole('button', { name: 'Save changes' });
    expect(save.getAttribute('aria-disabled')).toBe('true');
    save.focus();
    expect((await screen.findAllByText('Not available yet')).length).toBeGreaterThan(0);
  });
});

describe('the tabs', () => {
  it('are an underline row of four links, the current one marked', async () => {
    await renderPage('tools');
    const views = screen.getByRole('navigation', { name: 'Agent views' });
    expect(within(views).getAllByRole('link')).toHaveLength(4);
    expect(within(views).getByRole('link', { name: 'Tools and access' }).getAttribute('aria-current')).toBe('page');
    expect(within(views).getByRole('link', { name: /^Runs/ }).textContent).toContain('41 this week');
    expect(views.className).toContain('border-b');
  });
});

describe('Reset to default', () => {
  const edit: Factory['can'] = ['item:create', 'factory:manage', 'definition:edit'];

  it('sits beside the prompt header next to Edit prompt, off with its reason when the prompt matches', async () => {
    await renderPage('overview', { can: edit });
    const prompt = within(screen.getByRole('region', { name: 'Prompt' }));
    const reset = await prompt.findByRole('button', { name: 'Reset to default' });
    await waitFor(() => expect(reset.getAttribute('aria-disabled')).toBe('true'));
    expect(prompt.getByRole('link', { name: 'Edit prompt' }).getAttribute('href')).toContain('agent=frontend-engineer');
    reset.focus();
    expect((await screen.findAllByText('The prompt already matches the running version')).length).toBeGreaterThan(0);
  });

  it('is Not available yet without definition:edit', async () => {
    await renderPage('overview');
    const reset = within(screen.getByRole('region', { name: 'Prompt' })).getByRole('button', {
      name: 'Reset to default',
    });
    expect(reset.getAttribute('aria-disabled')).toBe('true');
    reset.focus();
    expect((await screen.findAllByText('Not available yet')).length).toBeGreaterThan(0);
  });

  it('offers the factory model beside an agent that sets its own, and not beside one that does not', async () => {
    await renderPage('overview', { can: edit });
    const settings = within(screen.getByRole('region', { name: 'Settings' }));
    expect(settings.getByRole('button', { name: 'Reset to default, claude-opus-5-5' }).className).toContain(
      'max-md:min-h-11',
    );
    cleanup();

    const { model: own, ...rest } = model.agents.agents[0] as Record<string, unknown>;
    expect(own).toBe('claude-haiku-5-5');
    await renderPage('overview', {
      can: edit,
      version: { model: { ...model, agents: { ...model.agents, agents: [rest] } } },
    });
    expect(screen.queryByRole('button', { name: /^Reset to default, / })).toBeNull();
  });
});

describe('the controls that are not built yet', () => {
  const button = () => screen.getByRole('button', { name: 'Ask Jonah for a fix' });

  it('keeps Ask Jonah for a fix "Not available yet" and sends no request', async () => {
    const { router } = await renderPage('overview', { can: ['item:create', 'factory:manage', 'definition:edit'] });
    const before = router.state.location.href;
    expect(button().getAttribute('aria-disabled')).toBe('true');

    fireEvent.click(button());
    fireEvent.keyDown(button(), { key: 'Enter' });
    expect(createDraftRequest).not.toHaveBeenCalled();
    expect(router.state.location.href).toBe(before);
  });

  it('shows "Not available yet" when Ask Jonah for a fix takes focus', async () => {
    await renderPage('overview');
    button().focus();
    expect((await screen.findAllByText('Not available yet')).length).toBeGreaterThan(0);
  });

  it('links the prompt file to Definition › Agents for this agent', async () => {
    await renderPage();
    const link = screen.getByRole('link', { name: 'Open prompt file' });
    expect(link.getAttribute('href')).toContain('/definition/agents');
    expect(link.getAttribute('href')).toContain('agent=frontend-engineer');
  });
});

describe('the states', () => {
  it('an agent without history says so, and its blocked calls read as none', async () => {
    await renderPage('tools', {
      reasons: false,
      agentStats: {
        ...stats,
        tasks_finished: 0,
        sent_back: { count: 0, ratio: null },
        spend_per_task_usd: null,
        median_turns: null,
        task_time_p85_minutes: null,
      },
    });
    expect(screen.getByText('Blocked this week: no calls.')).toBeTruthy();
  });

  it('an agent without history reads its empty reasons', async () => {
    await renderPage('overview', { reasons: false });
    expect(screen.getByText('Nothing was sent back in the last 7 days.')).toBeTruthy();
  });

  it('the settings wait for the version that runs, and say so when it cannot be read', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(() => new Promise(() => {}));
    const { unmount } = await renderPage('overview', { version: false });
    expect(screen.getByText('Loading settings').closest('[aria-busy="true"]')).not.toBeNull();
    unmount();
    cleanup();

    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new TypeError('Failed to fetch'));
    await renderPage('overview', { version: false });
    await waitFor(() => expect(screen.getByText('The version that runs can’t be read right now.')).toBeTruthy());
  });
});

describe('Bake off this agent', () => {
  const editor = ['item:create', 'factory:manage', 'definition:edit'];
  const button = () => screen.getByRole('button', { name: 'Bake off this agent' });

  it('starts a bake-off of the agent and opens the new one', async () => {
    vi.mocked(startBakeOff).mockResolvedValue({
      bake_off: { id: 'bake-off-2', state: 'queued' } as never,
      estimate: { usd: 15.6, unpriced: [] },
    });
    const { router } = await renderPage('overview', { can: editor });

    fireEvent.click(button());

    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/f/goal/jonah/bake-offs/bake-off-2'));
    expect(startBakeOff).toHaveBeenCalledWith('org-1', 'factory-1', 'frontend-engineer');
  });

  it('is busy and disabled while the start is pending', async () => {
    vi.mocked(startBakeOff).mockReturnValue(new Promise(() => {}));
    await renderPage('overview', { can: editor });

    fireEvent.click(button());

    await waitFor(() => expect(button().getAttribute('aria-busy')).toBe('true'));
    expect((button() as HTMLButtonElement).disabled).toBe(true);
  });

  it('is disabled with its reason without definition:edit, and starts nothing', async () => {
    await renderPage('overview');

    expect((button() as HTMLButtonElement).disabled).toBe(true);
    const reason = screen.getByText('View only. Editors of goal change its definition.', {
      selector: `#${button().getAttribute('aria-describedby')}`,
    });
    expect(button().getAttribute('aria-describedby')).toBe(reason.id);

    fireEvent.click(button());
    expect(startBakeOff).not.toHaveBeenCalled();
  });

  it('shows a 409 learning-budget refusal with its estimate, and says nothing started', async () => {
    const refused = new ApiError({
      title: 'Conflict',
      status: 409,
      code: 'learning-budget-exhausted',
      estimate: { usd: 15.6, unpriced: [] },
    } as unknown as Problem);
    vi.mocked(startBakeOff).mockRejectedValue(refused);
    const { router } = await renderPage('overview', { can: editor });

    fireEvent.click(button());

    expect(await screen.findByText(/would cost about \$15\.60/)).toBeTruthy();
    expect(screen.getByText(/Nothing was started/)).toBeTruthy();
    expect(router.state.location.pathname).toBe('/o/acme/f/goal/agents/frontend-engineer');
  });

  it('says the budget has run out when a 409 carries no estimate', async () => {
    vi.mocked(startBakeOff).mockRejectedValue(
      new ApiError({ title: 'Conflict', status: 409, code: 'learning-budget-exhausted' } as unknown as Problem),
    );
    await renderPage('overview', { can: editor });

    fireEvent.click(button());

    expect(await screen.findByText('The learning budget has run out. Nothing was started.')).toBeTruthy();
  });

  it('shows another refusal in place with its detail', async () => {
    vi.mocked(startBakeOff).mockRejectedValue(
      new ApiError({
        title: 'Unprocessable',
        status: 422,
        code: 'invalid-bake-off',
        detail: 'the version has no such agent',
      } as unknown as Problem),
    );
    await renderPage('overview', { can: editor });

    fireEvent.click(button());

    expect(await screen.findByText('Not started: the version has no such agent')).toBeTruthy();
  });

  it('shows a network failure in place, and says nothing started', async () => {
    vi.mocked(startBakeOff).mockRejectedValue(new TypeError('Failed to fetch'));
    await renderPage('overview', { can: editor });

    fireEvent.click(button());

    expect(await screen.findByText('goald is not answering. Nothing started; try again once it is.')).toBeTruthy();
  });
});

describe('phone tap targets on the agent page', () => {
  it('gives each rejection-reason button the touchLink classes', async () => {
    await renderPage('overview');
    const reasons = screen.getByRole('region', { name: 'Why review sent work back' });
    const button = within(reasons).getByRole('button', { name: /No test for a new empty or error state/ });
    for (const cls of touchLink.split(' ')) expect(button.classList.contains(cls)).toBe(true);
  });
});
