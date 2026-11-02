// @vitest-environment jsdom
import { cleanup, fireEvent, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import type { BuilderMeta, Factory } from '../../api/types';
import { renderBoardPage } from '../../test/route';
import type { CheckEntry, Entry, NoteEntry, RunEntry, Trace } from './timeline';
import { TraceTab } from './trace-tab';

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
  models: [{ id: 'claude-opus-5-5', label: 'Claude Opus 5.5', default: true }],
  capabilities: { tool_approvals: false },
  limits: { skill_bytes: 1_000_000, skill_files: 50 },
};

const specRun: RunEntry = {
  kind: 'run',
  key: 'run-a',
  id: 'a',
  at: '2026-10-25T08:00:00Z',
  endedAt: '2026-10-25T08:06:00Z',
  station: 'spec',
  agent: 'spec-writer',
  model: 'claude-opus-5-5',
  version: 12,
  status: 'succeeded',
  turns: 4,
  costUsd: 0.41,
  tools: [
    {
      id: 't1',
      tool: 'Read',
      argument: 'src/deliver.ts',
      summary: 'Read 80 lines',
      isError: false,
      at: '2026-10-25T08:01:00Z',
    },
  ],
};
const buildRun: RunEntry = {
  kind: 'run',
  key: 'run-b',
  id: 'b',
  at: '2026-10-25T08:10:00Z',
  endedAt: '2026-10-25T08:20:00Z',
  station: 'build',
  agent: 'builder',
  status: 'failed',
  costUsd: 1.92,
  tools: [
    {
      id: 't2',
      tool: 'Bash',
      argument: 'pnpm test',
      summary: 'FAIL retry.test.ts',
      isError: true,
      at: '2026-10-25T08:12:00Z',
    },
  ],
};
const check: CheckEntry = {
  kind: 'check',
  key: 'check-1',
  at: '2026-10-25T08:05:00Z',
  station: 'spec',
  command: 'spec contract',
  passed: true,
  durationMs: 2000,
};
const touch: NoteEntry = {
  kind: 'note',
  key: 'note-1',
  at: '2026-10-25T08:15:00Z',
  station: 'build',
  actor: 'Maya Chen',
  text: 'Maya Chen answered a question',
  tone: 'you',
};

function trace(entries: Entry[]): Trace {
  return { entries, costUsd: 2.33, turns: 4 };
}

function renderTab(
  t: Trace,
  props: { itemState?: 'in_progress' | 'done'; status?: 'loading' | 'error' | 'ready'; step?: number } = {},
) {
  return renderBoardPage({
    page: () => (
      <TraceTab
        trace={t}
        itemState={props.itemState ?? 'done'}
        status={props.status ?? 'ready'}
        now={Date.parse('2026-10-25T08:30:00Z')}
        step={props.step}
      />
    ),
    org,
    factory,
    builderMeta,
  });
}

describe('TraceTab', () => {
  it('lists every run oldest first with its tool calls, checks and human touches, and a legend', async () => {
    renderTab(trace([specRun, check, buildRun, touch]));

    const rows = await screen.findAllByRole('button');
    const names = rows.map((r) => r.textContent ?? '');
    expect(names.findIndex((n) => n.includes('Spec'))).toBeLessThan(names.findIndex((n) => n.includes('Build')));
    expect(screen.getByRole('button', { name: /Read\s*src\/deliver\.ts/ })).toBeTruthy();
    expect(screen.getByRole('button', { name: /spec contract passed/ })).toBeTruthy();
    expect(screen.getByRole('button', { name: /Maya Chen answered a question/ })).toBeTruthy();
    const legend = screen.getByRole('list', { name: 'Legend' });
    expect(
      within(legend)
        .getAllByRole('listitem')
        .map((l) => l.textContent),
    ).toEqual(['Run', 'Tool call', 'Running', 'Check passed', 'Failed', 'A person']);
  });

  it('shows the selected step in the side panel, the latest step first', async () => {
    renderTab(trace([specRun, buildRun]));

    const panel = await screen.findByRole('complementary');
    expect(within(panel).getByRole('heading', { name: 'Bash' })).toBeTruthy();
    expect(within(panel).getByText('FAIL retry.test.ts')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: /Read\s*src\/deliver\.ts/ }));
    expect(within(screen.getByRole('complementary')).getByRole('heading', { name: 'Read' })).toBeTruthy();
    expect(screen.getByRole('button', { name: /Read\s*src\/deliver\.ts/ }).getAttribute('aria-pressed')).toBe('true');
  });

  it('selects the step of the event a link names, and the latest step without one', async () => {
    const failed: CheckEntry = { ...check, key: 'ev-41', passed: false, command: 'unit tests' };
    renderTab(trace([specRun, failed, buildRun]), { step: 41 });

    const panel = await screen.findByRole('complementary');
    expect(within(panel).getByRole('heading', { name: 'Check' })).toBeTruthy();
  });

  it('keeps the live line in its header while a run works', async () => {
    const { endedAt: _end, ...rest } = buildRun;
    renderTab(trace([specRun, { ...rest, status: 'running' }]), { itemState: 'in_progress' });

    expect(await screen.findByText('Builder working, 1 tool calls')).toBeTruthy();
  });

  it('says there are no tool calls when the item has no runs', async () => {
    renderTab(trace([]));

    expect(await screen.findByText('No tool calls yet.')).toBeTruthy();
  });

  it('shows the skeleton as busy while the runs load', async () => {
    renderTab(trace([]), { status: 'loading' });

    expect(await screen.findByText('Loading')).toBeTruthy();
    expect(document.querySelector('[aria-busy="true"]')).not.toBeNull();
  });
});
