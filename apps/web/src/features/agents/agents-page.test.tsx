// @vitest-environment jsdom
import { cleanup, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError, getAgentStats } from '../../api/client';
import { agentStatsQuery, versionModelQuery } from '../../api/queries';
import type { AgentStats, Factory, OrgSummary, VersionModel } from '../../api/types';
import { renderFactoryPage, type Seed } from '../../test/route';
import { AgentsPage } from './agents-page';

// The statistics read goes through the client, so an error test can fail that one request.
vi.mock('../../api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../api/client')>();
  return { ...actual, getAgentStats: vi.fn(actual.getAgentStats) };
});

afterEach(cleanup);

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: null };
const factory: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'widgets',
  title: 'Widgets',
  work_types: [],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'approver',
  can: ['item:operate', 'item:decide'],
};

// Fixture names of their own: the line is read from the model, so the page names nothing.
const line = {
  basics: { size: 'large' },
  connections: [{ name: 'github' }, { name: 'context7' }],
  line: {
    flow: { constraint: 'build', andon: { rework_rate: 0.4 } },
    stations: [
      { id: 'inbox', name: 'Inbox', kind: 'queue' },
      { id: 'draft', name: 'Draft', kind: 'agent', agent: 'drafter', wip: 2 },
      {
        id: 'build',
        name: 'Build',
        kind: 'agent',
        route: { alpha: 'coder-one', beta: 'coder-two' },
        wip: 4,
        buffer: 2,
      },
      { id: 'check', name: 'Check', kind: 'agent', agent: 'checker', wip: 1 },
      { id: 'finish', name: 'Finish', kind: 'human' },
      { id: 'done', name: 'Done', kind: 'done' },
    ],
  },
  agents: {
    default_model: 'claude-opus-5-5',
    agents: [
      { name: 'drafter', description: 'Writes the outline.', per_item_usd: 6, skills: ['house-style'] },
      {
        name: 'coder-one',
        description: 'Writes the change.',
        per_item_usd: 20,
        connections: ['github'],
        skills: [],
      },
      {
        name: 'coder-two',
        description: 'Writes the other change.',
        model: 'claude-haiku-5-5',
        per_item_usd: 20,
        connections: ['github', 'context7'],
        skills: [],
      },
      { name: 'checker', description: 'Checks the change against the outline.', per_item_usd: 12, skills: [] },
    ],
  },
  review: { budget: { per_item_usd: 12, max_turns_per_task: 150 } },
};

/** The running version's answer: its model and the ranges of its lines (the query's shape). */
const version = (value: unknown) => ({ model: value, ranges: {} }) as unknown as VersionModel;

const stats = (agent: string, over: Partial<AgentStats> = {}): AgentStats => ({
  agent,
  from: '2026-10-20',
  to: '2026-10-27',
  model: 'claude-opus-5-5',
  stations: [],
  tasks_finished: 10,
  sent_back: { count: 0, ratio: 0 },
  median_turns: null,
  spend_per_task_usd: null,
  task_time_p85_minutes: null,
  spend_usd: 0,
  reasons: [],
  denied_tool_calls: { count: 0, tools: [] },
  ...over,
});

/** The running version's model and each agent's statistics, as the route's loader leaves them. */
function seeded(running: unknown, agentStats: AgentStats[]): Seed[] {
  return [
    [versionModelQuery(org.id, factory.id, 'current').queryKey, version(running)],
    ...agentStats.map((s): Seed => [agentStatsQuery(org.id, factory.id, s.agent).queryKey, s]),
  ];
}

const readyStats = [
  stats('drafter', { tasks_finished: 24, sent_back: { count: 1, ratio: 0.04 } }),
  stats('coder-one', { tasks_finished: 19, sent_back: { count: 2, ratio: 0.12 } }),
  stats('coder-two', { model: 'claude-haiku-5-5', tasks_finished: 17, sent_back: { count: 3, ratio: 0.18 } }),
  stats('checker', { tasks_finished: 28 }),
];

function renderPage(seed: Seed[]) {
  return renderFactoryPage({ page: AgentsPage, path: 'agents/', org, factory, seed });
}

describe('AgentsPage', () => {
  it("shows the line with each station's agents and the factory defaults", async () => {
    renderPage(seeded(line, readyStats));

    expect(await screen.findByRole('heading', { level: 1, name: 'Agents' })).toBeTruthy();
    expect(
      screen.getByText("4 roles and 4 agents staff Widgets's line. Each station decides who works on an item."),
    ).toBeTruthy();

    const defaults = within(screen.getByRole('region', { name: 'Factory defaults' }));
    expect(defaults.getByText('claude-opus-5-5')).toBeTruthy();
    expect(defaults.getByText('Large')).toBeTruthy();
    expect(defaults.getByText('$12.00')).toBeTruthy();
    expect(defaults.getByText('150')).toBeTruthy();
    expect(defaults.getByText('github, context7')).toBeTruthy();
    expect(defaults.getByRole('link', { name: 'Edit defaults' }).getAttribute('href')).toBe(
      '/o/acme/f/widgets/definition/basics',
    );

    const stations = screen
      .getAllByRole('heading', { level: 2 })
      .map((heading) => heading.textContent?.replace('Constraint', '').trim());
    expect(stations).toEqual(['Factory defaults', 'Inbox', 'Draft', 'Build', 'Check', 'Finish', 'Done']);

    const build = screen.getByRole('heading', { level: 2, name: /Build/ }).closest('li') as HTMLElement;
    expect(within(build).getByText('Constraint')).toBeTruthy();
    expect(within(build).getByText('alpha')).toBeTruthy();
    expect(within(build).getByText('beta')).toBeTruthy();
    expect(
      within(build)
        .getByRole('link', { name: /coder-one/ })
        .getAttribute('href'),
    ).toBe('/o/acme/f/widgets/agents/coder-one');
  });

  it("shows each card's model, its default or own model, its cap and its statistics", async () => {
    renderPage(seeded(line, readyStats));

    const drafter = (await screen.findByRole('link', { name: /drafter/ })) as HTMLElement;
    expect(within(drafter).getByText('Writes the outline.')).toBeTruthy();
    expect(within(drafter).getByText('Default')).toBeTruthy();
    expect(within(drafter).getByText('Cap $6.00')).toBeTruthy();
    expect(within(drafter).getByText('Sent back')).toBeTruthy();
    expect(within(drafter).getByText('4%')).toBeTruthy();
    expect(within(drafter).getByText('24')).toBeTruthy();

    const coderTwo = screen.getByRole('link', { name: /coder-two/ });
    expect(within(coderTwo).getByText('Own model')).toBeTruthy();
    expect(within(coderTwo).getByText('claude-haiku-5-5')).toBeTruthy();
    expect(within(coderTwo).getByText('18%')).toBeTruthy();
  });

  it('shows no copy number on a card and says where a station with no agent waits', async () => {
    renderPage(seeded(line, readyStats));

    await screen.findByRole('link', { name: /drafter/ });
    expect(screen.queryByText(/\b\d+ copies\b/)).toBeNull();
    expect(screen.getByText('Items wait here for drafter')).toBeTruthy();
  });

  it('shows the loading state until the definition and the statistics arrive', async () => {
    const { container } = renderPage([]);

    expect(await screen.findByText('Loading')).toBeTruthy();
    expect(container.querySelector('[aria-busy="true"]')).not.toBeNull();
  });

  it('shows the error state with a retry when the statistics cannot be read', async () => {
    // Every agent's statistics request fails; the other tests seed theirs, so none reaches the client.
    // A client error is not retried, so the error shows at once (the query retries other failures).
    const refused = new ApiError({ title: 'Forbidden', status: 403 } as unknown as ConstructorParameters<
      typeof ApiError
    >[0]);
    vi.mocked(getAgentStats).mockRejectedValue(refused);
    const actual = await vi.importActual<typeof import('../../api/client')>('../../api/client');

    try {
      renderPage(seeded(line, []).slice(0, 1));

      expect(await screen.findByRole('button', { name: 'Try again' })).toBeTruthy();
    } finally {
      vi.mocked(getAgentStats).mockImplementation(actual.getAgentStats);
    }
  });

  it('shows the stations and the empty copy when the definition has no agents', async () => {
    const empty = { ...line, agents: { agents: [] } };
    renderPage(seeded(empty, []));

    expect(
      await screen.findByText("No agents staff Widgets's line yet. Each station decides who works on an item."),
    ).toBeTruthy();
    expect(screen.getByRole('heading', { level: 2, name: /Draft/ })).toBeTruthy();
    expect(screen.getAllByText('No agent').length).toBeGreaterThan(0);
    expect(screen.queryByRole('link', { name: /drafter/ })).toBeNull();
  });
});
