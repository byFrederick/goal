// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  agentStatsQuery,
  builderMetaQuery,
  versionFileQuery,
  versionModelQuery,
  versionQuery,
} from '../../api/queries';
import type { AgentStats, Factory, OrgSummary } from '../../api/types';
import { renderFactoryPage, type Seed } from '../../test/route';
import { TooltipProvider } from '../../ui/tooltip';
import { AgentPage } from './agent-page';
import type { AgentView } from './views';

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
        prompt: { path: `./${promptPath}` },
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
    [builderMetaQuery(org.id).queryKey, { capabilities: { tool_approvals: false } }],
    [versionQuery(org.id, factory.id, 'current').queryKey, { files: [{ path: promptPath, sha256: 'sha-prompt' }] }],
    [versionFileQuery(org.id, factory.id, 'current', promptPath, 'sha-prompt').queryKey, { text: promptText }],
  ];
  // `version: false` leaves the version that runs unread, as the page sees it while it loads.
  if (version !== false) seed.push([versionModelQuery(org.id, factory.id, 'current').queryKey, version]);
  return seed;
}

/** Renders the page and waits for its first render: the router mounts the page after its match. */
async function renderPage(view: AgentView = 'overview', options: Parameters<typeof seeds>[0] = {}) {
  const result = renderFactoryPage({
    page: () => (
      <TooltipProvider>
        <AgentPage agent="frontend-engineer" view={view} />
      </TooltipProvider>
    ),
    path: 'agents/frontend-engineer',
    org,
    factory,
    seed: seeds(options),
    search: view === 'overview' ? undefined : { view },
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
    ).toEqual(['Overview', 'Prompt', 'Tools and access', 'Runs']);
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

describe('the controls that are not built yet', () => {
  it.each(['Bake off this agent', 'Ask Jonah for a fix'])('%s is aria-disabled and does nothing', async (name) => {
    const { router } = await renderPage();
    const before = router.state.location.href;
    const control = screen.getByRole('button', { name });
    expect(control.getAttribute('aria-disabled')).toBe('true');

    fireEvent.click(control);
    fireEvent.keyDown(control, { key: 'Enter' });
    fireEvent.keyDown(control, { key: ' ' });
    expect(router.state.location.href).toBe(before);
  });

  it('shows "Not available yet" when a control takes focus', async () => {
    await renderPage();
    screen.getByRole('button', { name: 'Bake off this agent' }).focus();
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
