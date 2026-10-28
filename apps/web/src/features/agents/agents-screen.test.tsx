// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import * as definitions from '../../api/definitions';
import { agentStatsQuery, defKeys, versionModelQuery } from '../../api/queries';
import type { AgentStats, Factory, OrgSummary, SaveResult, VersionModel } from '../../api/types';
import { renderFactoryPage, type Seed } from '../../test/route';
import { AgentsPage } from './agents-page';

// A small running version: one agent staffing the build station, and the done station.
const { running } = vi.hoisted(() => ({
  running: {
    basics: { size: 'large' },
    connections: [],
    line: {
      stations: [
        { id: 'build', name: 'Build', kind: 'agent', agent: 'coder', wip: 2 },
        { id: 'done', name: 'Done', kind: 'done' },
      ],
    },
    agents: { agents: [{ name: 'coder', description: 'Writes the change.', prompt: { path: './agents/coder.md' } }] },
    review: {},
  },
}));

// The save goes to goald's draft endpoints; the New agent flow opens the draft, writes the agent and its
// prompt file, then moves to the definition's agents section on the new agent.
vi.mock('../../api/definitions', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../api/definitions')>();
  const saved = {
    revision: 2,
    etag: 'e2',
    updated_at: '2026-10-27T09:00:00Z',
    change_count: 1,
    problem_count: 0,
    sections: [],
  } satisfies SaveResult;
  return {
    ...actual,
    openDraft: vi.fn(async () => undefined),
    getDraftModel: vi.fn(async () => ({
      revision: 1,
      model: running,
      etags: { basics: 'b1', line: 'l1', agents: 'a1', connections: 'c1', review: 'r1' },
      edited: {},
      ranges: {},
    })),
    saveDraftSection: vi.fn(async () => saved),
    putDraftFile: vi.fn(async () => saved),
  };
});

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: 'member' };
const factoryWith = (can: string[]): Factory => ({
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
  role: 'editor',
  can,
});

const stats = (agent: string): AgentStats => ({
  agent,
  from: '2026-10-20',
  to: '2026-10-27',
  model: 'claude-opus-5-5',
  stations: [],
  tasks_finished: 3,
  sent_back: { count: 0, ratio: 0 },
  median_turns: null,
  spend_per_task_usd: null,
  task_time_p85_minutes: null,
  spend_usd: 0,
  reasons: [],
  denied_tool_calls: { count: 0, tools: [] },
});

function renderAgents(can: string[]) {
  const factory = factoryWith(can);
  const seed: Seed[] = [
    // The factory's detail says it has no draft, so the editor session shows the running version.
    [defKeys.factory(org.id, factory.id), factory],
    [
      versionModelQuery(org.id, factory.id, 'current').queryKey,
      { model: running, ranges: {} } as unknown as VersionModel,
    ],
    [agentStatsQuery(org.id, factory.id, 'coder').queryKey, stats('coder')],
  ];
  return renderFactoryPage({ page: AgentsPage, path: 'agents/', org, factory, seed, topBar: true });
}

describe('New agent on the Agents screen', () => {
  it('enables New agent for a caller who may edit the definition', async () => {
    renderAgents(['definition:edit']);

    const button = await screen.findByRole('button', { name: 'New agent' });
    expect(button.getAttribute('aria-disabled')).toBeNull();
    expect((button as HTMLButtonElement).disabled).toBe(false);
  });

  it('disables New agent with the not-available tip for a viewer', async () => {
    renderAgents([]);

    const button = await screen.findByRole('button', { name: 'New agent' });
    expect(button.getAttribute('aria-disabled')).toBe('true');
    fireEvent.click(button);
    expect(screen.queryByRole('dialog', { name: 'Add agent' })).toBeNull();
  });

  it('opens the Add agent name dialog, refuses a name that is taken, and adds the agent', async () => {
    const { router } = renderAgents(['definition:edit']);

    fireEvent.click(await screen.findByRole('button', { name: 'New agent' }));
    const dialog = await screen.findByRole('dialog', { name: 'Add agent' });

    fireEvent.change(dialog.querySelector('input') as HTMLInputElement, { target: { value: 'coder' } });
    fireEvent.click(screen.getByRole('button', { name: 'Add agent' }));
    expect(await screen.findByText('coder is already an agent of this factory.')).toBeTruthy();

    fireEvent.change(dialog.querySelector('input') as HTMLInputElement, { target: { value: 'cost-analyst' } });
    fireEvent.click(screen.getByRole('button', { name: 'Add agent' }));

    // The agent and its prompt file go out in one section save, the agents section with the file.
    await waitFor(() => expect(definitions.saveDraftSection).toHaveBeenCalled());
    const [, , section, , save] = vi.mocked(definitions.saveDraftSection).mock.calls[0] ?? [];
    expect(section).toBe('agents');
    expect(Object.keys(save?.files ?? {})).toContain('agents/cost-analyst.md');
    await waitFor(() => expect(router.state.location.pathname).toMatch(/\/definition\/agents$/));
    expect(router.state.location.search).toMatchObject({ agent: 'cost-analyst' });
  });
});
