// @vitest-environment jsdom
import { cleanup, screen } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { itemRunsQuery, membersQuery, versionFileQuery, versionModelQuery, versionQuery } from '../../api/queries';
import type { Factory, Item, ItemEvent, Member, Run } from '../../api/types';
import { renderFactoryPage, type Seed } from '../../test/route';
import { CostCard, DetailsCard, OverlapsCard, RulesCard } from './side-cards';
import { buildTrace } from './timeline';

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const org = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: null };
const factory: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'software',
  title: 'Software',
  work_types: ['feature', 'fix'],
  stations: [
    { id: 'spec', kind: 'agent' },
    { id: 'build', kind: 'agent' },
  ],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'approver',
  can: ['item:operate', 'item:decide'],
};

const ownerId = 'user-freddy';
const members = [{ user: { id: ownerId, name: 'Freddy Cruz', email: 'f.cruz@example.com' } }] as Member[];

/** The item #147 of the design, at Build with a frontend lane, an overlap with #145 and a PR. */
function item(overrides: Partial<Item> = {}): Item {
  return {
    id: 'item-147',
    ref: 'software#147',
    factory_id: 'factory-1',
    number: 147,
    type: 'feature',
    lane: 'frontend',
    title: "Show this week's spend in the sidebar",
    body: '',
    station: 'build',
    state: 'queued',
    class_of_service: 'standard',
    priority: 0,
    rework_count: 0,
    cost_usd: 3,
    outputs: {
      build: { pull_request: 'https://github.com/acme/goal/pull/318', branch: 'goal/147-sidebar-spend' },
    },
    created_at: '2026-10-25T08:00:00Z',
    updated_at: '2026-10-25T08:05:00Z',
    flow: {
      spans: [],
      target_seconds: null,
      rework_target: null,
      budget: { per_item_usd: 12, max_turns_per_task: 80 },
      current_run: null,
    },
    waiting: {
      kind: 'files',
      items: ['software#145'],
      files: ['features/usage/'],
      needs: [{ ref: 'software#145', title: 'Check the spend against the budget', paths: ['features/usage/'] }],
    },
    ...overrides,
  };
}

const events: ItemEvent[] = [
  {
    id: 1,
    item_id: 'item-147',
    at: '2026-10-25T07:00:00Z',
    kind: 'created',
    station: 'spec',
    actor: 'engine',
    data: { actor: `user:${ownerId}` },
  },
  {
    id: 2,
    item_id: 'item-147',
    at: '2026-10-25T08:00:00Z',
    kind: 'run_started',
    station: 'build',
    actor: 'frontend-engineer',
    run_id: 'run-build',
    data: {},
  },
];

const runs: Run[] = [runOf('spec', 'spec-writer', 0.62), runOf('build', 'frontend-engineer', 2.38)];

function runOf(station: string, agent: string, cost: number): Run {
  return {
    id: `run-${station}`,
    item_id: 'item-147',
    station,
    agent,
    model: 'claude-haiku-5-5',
    status: 'succeeded',
    turns: 3,
    input_tokens: 0,
    output_tokens: 0,
    cost_usd: cost,
    error: null,
    started_at: '2026-10-25T07:30:00Z',
    ended_at: '2026-10-25T07:40:00Z',
    definition_version_id: null,
    definition_version: null,
  };
}

/** A model with the agent that works the item and the rules its factory has (each rule's file is seeded below). */
const model = {
  line: {
    stations: [
      { id: 'spec', kind: 'agent', agent: 'spec-writer' },
      { id: 'build', name: 'Build', kind: 'agent', agent: 'frontend-engineer' },
    ],
  },
  agents: {
    agents: [
      {
        name: 'frontend-engineer',
        skills: Array.from({ length: 18 }, (_, i) => `skill-${i}`),
      },
    ],
    rules: [
      { name: 'evidence', source: './rules/evidence.md' },
      { name: 'cost', source: './rules/cost.md' },
    ],
  },
};

const ruleTexts: Record<string, string> = {
  evidence: '---\ndescription: Mark a criterion met only with evidence from this run.\n---\n\nBody.\n',
  cost: '---\ndescription: Read only what the task needs.\nappliesTo:\n  lanes: [backend]\n---\n\nBody.\n',
};

/** The seeds the cards read through the query cache: members, the runs and the version's model and files. */
function seeds(withModel = true): Seed[] {
  const list: Seed[] = [
    [membersQuery(org.id).queryKey, members],
    [itemRunsQuery(org.id, 'software#147').queryKey, { runs }],
  ];
  if (withModel) {
    const versionFiles = [
      { path: 'rules/evidence.md', sha256: 'sha-evidence', size: 80 },
      { path: 'rules/cost.md', sha256: 'sha-cost', size: 80 },
    ];
    list.push(
      [versionModelQuery(org.id, factory.id, 'current').queryKey, { model, ranges: {} }],
      [versionQuery(org.id, factory.id, 'current').queryKey, { files: versionFiles }],
    );
    for (const [name, text] of Object.entries(ruleTexts)) {
      list.push([
        versionFileQuery(org.id, factory.id, 'current', `rules/${name}.md`, `sha-${name}`).queryKey,
        { text, sha256: `sha-${name}` },
      ]);
    }
  }
  return list;
}

function trace() {
  return buildTrace(events, runs);
}

function renderCards(card: () => ReactNode, withModel = true) {
  return renderFactoryPage({
    page: card,
    path: 'items/$ref',
    location: 'items/147',
    org,
    factory,
    seed: seeds(withModel),
  });
}

describe('DetailsCard', () => {
  it('shows the owner, the agent by role, its model, the station with its lane, the branch and the pull request', async () => {
    renderCards(() => <DetailsCard item={item()} factory={factory} events={events} trace={trace()} />);

    const details = await screen.findByRole('region', { name: 'Details' });
    expect(details.textContent).toContain('OwnerFreddy Cruz');
    expect(details.textContent).toContain('Working on itFrontend engineer');
    expect(details.textContent).toContain('Modelclaude-haiku-5-5');
    expect(details.textContent).toContain('StationBuild, frontend lane');
    expect(details.textContent).toContain('Branchgoal/147-sidebar-spend');
    expect(screen.getByRole('link', { name: '#318' }).getAttribute('href')).toBe(
      'https://github.com/acme/goal/pull/318',
    );
  });

  it('names the run by its role, never by its numbered agent copy', async () => {
    renderCards(() => <DetailsCard item={item()} factory={factory} events={events} trace={trace()} />);

    await screen.findByRole('region', { name: 'Details' });
    expect(document.body.textContent).not.toMatch(/frontend-engineer-\d/);
  });

  it('leaves out the rows it has no data for', async () => {
    const { lane: _lane, ...unlaned } = item();
    renderCards(() => (
      <DetailsCard item={{ ...unlaned, outputs: {} }} factory={factory} events={[]} trace={buildTrace([])} />
    ));

    const details = await screen.findByRole('region', { name: 'Details' });
    expect(details.textContent).toContain('StationBuild');
    for (const label of ['Owner', 'Working on it', 'Model', 'Branch', 'Pull request', 'Came from']) {
      expect(details.textContent).not.toContain(label);
    }
  });

  it('says which assembly the item is a part of, with the assembly ref linked', async () => {
    const part = item({
      assembly: { id: 'assembly-150', ref: 'software#150', title: 'Usage and budgets', part: 4, parts: 4 },
    });
    renderCards(() => <DetailsCard item={part} factory={factory} events={events} trace={buildTrace([])} />);

    expect(await screen.findByRole('link', { name: 'software#150' })).toBeTruthy();
    expect(screen.getByRole('region', { name: 'Details' }).textContent).toContain('Part of');
  });
});

describe('CostCard', () => {
  it('shows the cost of the cap, a bar of each station and the legend, and that it stops at the cap', async () => {
    renderCards(() => <CostCard item={item()} factory={factory} runs={runs} />);

    const cost = await screen.findByRole('region', { name: 'Cost' });
    expect(cost.textContent).toContain('$3.00 of $12');
    expect(screen.getByRole('img', { name: 'Spec $0.62, Build $2.38 of a $12 cap' })).toBeTruthy();
    expect(cost.textContent).toContain('Spec $0.62');
    expect(cost.textContent).toContain('Build $2.38');
    expect(cost.textContent).toContain('Stops at the cap');
  });

  it('shows the cost and the legend alone without a cap', async () => {
    const uncapped = item({
      flow: {
        spans: [],
        target_seconds: null,
        rework_target: null,
        budget: { per_item_usd: null, max_turns_per_task: null },
        current_run: null,
      },
    });
    renderCards(() => <CostCard item={uncapped} factory={factory} runs={runs} />);

    const cost = await screen.findByRole('region', { name: 'Cost' });
    expect(cost.textContent).toContain('$3.00');
    expect(cost.textContent).not.toContain(' of ');
    expect(cost.textContent).toContain('Build $2.38');
    expect(cost.textContent).not.toContain('Stops at the cap');
    expect(screen.queryByRole('img')).toBeNull();
  });
});

describe('OverlapsCard', () => {
  it('says what the overlapping item changes and that Goal merges it first', async () => {
    renderCards(() => <OverlapsCard item={item()} />);

    const overlaps = await screen.findByRole('region', { name: 'Overlaps' });
    expect(overlaps.textContent).toContain('#145');
    expect(overlaps.textContent).toContain('Also changes features/usage/. Goal merges #145 first.');
  });

  it('is not rendered without needs', async () => {
    const { waiting: _waiting, ...unwaited } = item();
    renderCards(() => <OverlapsCard item={unwaited} />);

    expect(screen.queryByRole('region', { name: 'Overlaps' })).toBeNull();
  });
});

describe('RulesCard', () => {
  it('shows the rules that apply to the agent here and the count of its skills', async () => {
    renderCards(() => <RulesCard item={item()} factory={factory} trace={trace()} />);

    const rules = await screen.findByRole('region', { name: 'Rules this run follows' });
    expect(rules.textContent).toContain('evidence');
    expect(rules.textContent).toContain('And 18 skills, loaded when the agent needs them');
  });

  it('leaves out a rule whose lanes do not include the item lane', async () => {
    renderCards(() => <RulesCard item={item({ lane: 'frontend' })} factory={factory} trace={trace()} />);

    const rules = await screen.findByRole('region', { name: 'Rules this run follows' });
    expect(rules.textContent).toContain('evidence');
    expect(rules.textContent).not.toContain('cost');
  });

  it('is busy with an sr-only Loading while the model loads', async () => {
    vi.spyOn(globalThis, 'fetch').mockReturnValue(new Promise(() => {}));
    renderCards(() => <RulesCard item={item()} factory={factory} trace={trace()} />, false);

    const rules = await screen.findByRole('region', { name: 'Rules this run follows' });
    expect(rules.getAttribute('aria-busy')).toBe('true');
    expect(screen.getByText('Loading')).toBeTruthy();
  });

  it('shows a short error line when the model fails to load', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new TypeError('Failed to fetch'));
    renderCards(() => <RulesCard item={item()} factory={factory} trace={trace()} />, false);

    expect(await screen.findByText('The rules could not be loaded.')).toBeTruthy();
  });
});
