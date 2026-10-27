// @vitest-environment jsdom
import { cleanup, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import type { BuilderMeta, Factory } from '../../api/types';
import { renderBoardPage } from '../../test/route';
import type { Entry } from './timeline';
import { TraceList } from './trace-view';

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

/** An investigator run on its second claim copy (slot 1, shown as neither "-1" nor "-2"), with one tool call. */
const run: Entry = {
  kind: 'run',
  key: 'run-a',
  id: 'a',
  at: '2026-10-25T08:00:00Z',
  endedAt: '2026-10-25T08:02:00Z',
  station: 'investigate',
  agent: 'investigator',
  slot: 1,
  model: 'claude-opus-5-5',
  status: 'succeeded',
  turns: 2,
  costUsd: 0.5,
  tools: [
    {
      id: 't1',
      tool: 'Read',
      argument: 'internal/core/engine/claim.go',
      summary: 'package engine',
      isError: false,
      at: '2026-10-25T08:00:30Z',
    },
  ],
};

describe('TraceList: a run', () => {
  it('names the run by its role and model, never by a numbered agent copy', async () => {
    renderBoardPage({
      page: () => <TraceList entries={[run]} itemState="done" />,
      org,
      factory,
      builderMeta,
    });

    expect(await screen.findByText('Investigator')).toBeTruthy();
    expect(screen.getByText(/Opus 5\.5/)).toBeTruthy();
    const tools = screen.getByRole('list', { name: 'Events by Investigator' });
    expect(within(tools).getByText(/Read/)).toBeTruthy();
    expect(document.body.textContent).not.toMatch(/investigator-\d/);
  });

  it('names the factory rules the run ran with, and shows none for a run without them', async () => {
    const withRules: Entry = {
      ...run,
      rules: [
        { name: 'secrets', sha256: 'aaa' },
        { name: 'delivery', sha256: 'bbb' },
      ],
    };
    renderBoardPage({
      page: () => <TraceList entries={[withRules]} itemState="done" />,
      org,
      factory,
      builderMeta,
    });

    const line = await screen.findByText('Ran with rules:', { exact: false });
    expect(line.textContent).toBe('Ran with rules: secrets, delivery');
    expect(within(line).getByText('secrets').getAttribute('translate')).toBe('no');
  });

  it('shows no rules line for a run that ran with none', async () => {
    renderBoardPage({
      page: () => <TraceList entries={[run]} itemState="done" />,
      org,
      factory,
      builderMeta,
    });

    expect(await screen.findByText('Investigator')).toBeTruthy();
    expect(screen.queryByText(/Ran with rules/)).toBeNull();
  });
});

describe('TraceList: a spec run and the timeline states', () => {
  const specRun: Entry = {
    ...run,
    key: 'run-spec',
    id: 'spec',
    station: 'spec',
    agent: 'architect',
    reply: {
      kind: 'output',
      value: { summary: 'Spend in the sidebar.', acceptance: ['Shows the amount', 'Turns amber at 80%'] },
    },
  };

  it('lists a spec run’s acceptance criteria under its heading', async () => {
    renderBoardPage({
      page: () => <TraceList entries={[specRun]} itemState="done" />,
      org,
      factory,
      builderMeta,
    });

    expect(await screen.findByText('Acceptance criteria')).toBeTruthy();
    expect(screen.getByText('Shows the amount')).toBeTruthy();
    expect(screen.getByText('Turns amber at 80%')).toBeTruthy();
  });

  it('says the run is waiting on the user when the item waits on them', async () => {
    renderBoardPage({
      page: () => <TraceList entries={[specRun]} itemState="waiting_human" />,
      org,
      factory,
      builderMeta,
    });

    expect(await screen.findByText('Waiting on you')).toBeTruthy();
  });

  it('shows an sr-only loading state while the runs load', async () => {
    renderBoardPage({
      page: () => <TraceList entries={[]} itemState="in_progress" status="loading" />,
      org,
      factory,
      builderMeta,
    });

    const busy = await screen.findByText('Loading');
    expect(busy.closest('[aria-busy="true"]')).toBeTruthy();
  });

  it('shows the empty state when nothing has happened yet', async () => {
    renderBoardPage({
      page: () => <TraceList entries={[]} itemState="queued" />,
      org,
      factory,
      builderMeta,
    });

    expect(await screen.findByText(/Nothing has happened yet/)).toBeTruthy();
  });

  it('says the runs could not load and still lists the events', async () => {
    renderBoardPage({
      page: () => <TraceList entries={[run]} itemState="done" status="error" />,
      org,
      factory,
      builderMeta,
    });

    expect(await screen.findByRole('alert')).toBeTruthy();
    expect(screen.getByText('Investigator')).toBeTruthy();
  });
});
