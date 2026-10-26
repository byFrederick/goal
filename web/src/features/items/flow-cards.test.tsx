// @vitest-environment jsdom
import { cleanup, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import type { BuilderMeta, Factory, Item } from '../../api/types';
import { renderBoardPage } from '../../test/route';
import { TimeCard } from './flow-cards';
import { buildTrace } from './timeline';

afterEach(cleanup);

const org = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: null };
const factory: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'sre',
  title: 'SRE',
  work_types: ['incident'],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'approver',
  can: ['item:operate', 'item:decide'],
};

const builderMeta: BuilderMeta = {
  models: [
    { id: 'claude-opus-5-5', label: 'Claude Opus 5.5', default: true },
    { id: 'claude-sonnet-5-5', label: 'Claude Sonnet 5.5', default: false },
  ],
  capabilities: { tool_approvals: false },
  limits: { skill_bytes: 1_000_000, skill_files: 50 },
};

/** An item in progress, with one run of its infra-engineer copy (slot 2) on turn 3 of an Opus 5.5 run. */
function workingItem(model: string): Item {
  return {
    id: 'item-1',
    ref: 'sre#1',
    factory_id: 'factory-1',
    number: 1,
    type: 'task',
    title: 'Roll out the new cache',
    body: '',
    station: 'apply',
    state: 'in_progress',
    class_of_service: 'standard',
    priority: 0,
    rework_count: 0,
    cost_usd: 0.42,
    outputs: {},
    created_at: '2026-10-25T08:00:00Z',
    updated_at: '2026-10-25T08:05:00Z',
    flow: {
      spans: [],
      target_seconds: null,
      rework_target: null,
      budget: { per_item_usd: null, max_turns_per_task: null },
      current_run: {
        id: 'run-1',
        agent: 'infra-engineer',
        slot: 2,
        model,
        turn: 3,
        started_at: '2026-10-25T08:04:00Z',
        output_contract: null,
        checks: [],
      },
    },
  };
}

describe('TimeCard: the run in progress', () => {
  it('names the run by its role and model, never by its numbered agent copy', async () => {
    renderBoardPage({
      page: () => <TimeCard item={workingItem('claude-opus-5-5')} factory={factory} trace={buildTrace([])} />,
      org,
      factory,
      builderMeta,
    });

    expect(await screen.findByText(/Infra engineer on turn 3/)).toBeTruthy();
    expect(screen.getByText('Opus 5.5')).toBeTruthy();
    expect(document.body.textContent).not.toContain('infra-engineer-2');
  });

  it('shows the model id until the install lists it', async () => {
    renderBoardPage({
      page: () => <TimeCard item={workingItem('claude-haiku-5-5')} factory={factory} trace={buildTrace([])} />,
      org,
      factory,
      builderMeta,
    });

    expect(await screen.findByText('claude-haiku-5-5')).toBeTruthy();
  });
});
