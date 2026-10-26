import { describe, expect, it } from 'vitest';
import type { ItemEvent } from '../../api/types';
import {
  activeRun,
  buildTrace,
  latestToolCalls,
  noteRecipient,
  type RunEntry,
  runRole,
  toolArgument,
} from './timeline';

let nextId = 1;
const ev = (kind: string, data: Record<string, unknown> = {}, extra: Partial<ItemEvent> = {}): ItemEvent => ({
  id: nextId++,
  item_id: 'item-1',
  at: `2026-10-23T14:${String(nextId).padStart(2, '0')}:00Z`,
  kind,
  station: 'build',
  actor: 'engine',
  data,
  ...extra,
});

describe('buildTrace', () => {
  it('groups a run with its tool calls paired to their results, and totals cost and turns', () => {
    const run = { run_id: 'r1', actor: 'builder-1' };
    const trace = buildTrace([
      ev('created', {}, { station: 'inbox' }),
      ev('moved', { from: 'inbox', to: 'build' }),
      ev('run_started', {}, run),
      ev('agent_started', { model: 'claude-sonnet-5-5' }, run),
      ev('tool_call', { id: 't1', tool: 'Bash', input: '{"command":"go test ./..."}' }, run),
      ev('tool_call', { id: 't2', tool: 'Read', input: '{"file_path":"cmd/goald/main.go"}' }, run),
      ev('tool_result', { id: 't2', is_error: false, summary: 'Read 214 lines' }, run),
      ev('tool_result', { id: 't1', is_error: true, summary: 'FAIL TestX' }, run),
      ev('usage', { input_tokens: 10 }, run),
      ev('run_finished', { status: 'succeeded', cost_usd: 1.25, turns: 12, error: '' }, run),
      ev('check_failed', { command: 'task lint', exit_code: 1, duration_ms: 4100 }),
    ]);

    const kinds = trace.entries.map((e) => e.kind);
    expect(kinds).toEqual(['note', 'note', 'run', 'check']);

    const r = trace.entries[2];
    expect(r?.kind).toBe('run');
    if (r?.kind !== 'run') return;
    expect(r.agent).toBe('builder-1');
    expect(r.model).toBe('claude-sonnet-5-5');
    expect(r.status).toBe('succeeded');
    expect(r.tools).toEqual([
      expect.objectContaining({ tool: 'Bash', argument: 'go test ./...', summary: 'FAIL TestX', isError: true }),
      expect.objectContaining({
        tool: 'Read',
        argument: 'cmd/goald/main.go',
        summary: 'Read 214 lines',
        isError: false,
      }),
    ]);
    expect(trace.costUsd).toBe(1.25);
    expect(trace.turns).toBe(12);
    expect(trace.model).toBe('claude-sonnet-5-5');

    const check = trace.entries[3];
    expect(check).toMatchObject({ kind: 'check', passed: false, command: 'task lint', exitCode: 1 });
  });

  it('keeps the reason a reviewer or a human sent work back, and marks it as rework', () => {
    const trace = buildTrace([
      ev('gate_rejected', { reason: 'Add a test for the TTL' }, { actor: 'reviewer' }),
      ev('rejected', { reason: 'Wrong lane' }, { actor: 'fcruz' }),
      ev('waiting_human'),
    ]);
    expect(trace.entries).toMatchObject([
      { text: 'reviewer sent it back', detail: 'Add a test for the TTL', tone: 'rework' },
      { text: 'fcruz sent it back', detail: 'Wrong lane', tone: 'rework' },
      { text: 'Waiting on you', tone: 'you' },
    ]);
  });

  it('shows a person’s note with its text, for the next run', () => {
    const trace = buildTrace([
      ev('note', { text: 'Also check the pool.', actor: 'user:local' }, { actor: 'Local user' }),
    ]);
    expect(trace.entries).toMatchObject([
      { kind: 'note', text: 'Local user left a note for the next run', detail: 'Also check the pool.' },
    ]);
  });

  it('addresses a note to the agent working the item, or the last one at its station', () => {
    const spec = { run_id: 's', actor: 'spec-writer', station: 'spec' };
    const build = { run_id: 'b', actor: 'builder', station: 'build' };
    const working = buildTrace([ev('run_started', {}, spec), ev('run_started', {}, build)]);
    expect(noteRecipient(working, 'build')).toBe('builder');

    const waiting = buildTrace([
      ev('run_started', {}, spec),
      ev('run_finished', { status: 'succeeded' }, spec),
      ev('moved', { from: 'spec', to: 'approve' }),
    ]);
    expect(noteRecipient(waiting, 'spec')).toBe('spec-writer');
    expect(noteRecipient(waiting, 'approve')).toBeUndefined();
  });

  it('shows event kinds it does not know instead of dropping them', () => {
    const trace = buildTrace([ev('pr_opened', {}, { actor: 'builder-1' })]);
    expect(trace.entries).toMatchObject([{ kind: 'note', text: 'Pr opened' }]);
  });

  it('finds the run still working and the latest tool calls across runs', () => {
    const a = { run_id: 'a', actor: 'builder-1' };
    const b = { run_id: 'b', actor: 'builder-1' };
    const trace = buildTrace([
      ev('tool_call', { id: '1', tool: 'Read', input: '{}' }, a),
      ev('run_finished', { status: 'failed', error: 'max turns' }, a),
      ev('tool_call', { id: '1', tool: 'Edit', input: '{}' }, b),
      ev('tool_call', { id: '2', tool: 'Bash', input: '{}' }, b),
    ]);
    expect(activeRun(trace)?.key).toBe('run-b');
    expect(latestToolCalls(trace, 2).map((t) => t.tool)).toEqual(['Edit', 'Bash']);
    expect(trace.entries[0]).toMatchObject({ status: 'failed', error: 'max turns' });
  });

  it('names each run by its agent copy: the slot of the claim it ran in', () => {
    const a = { run_id: 'a', actor: 'investigator' };
    const retry = { run_id: 'b', actor: 'investigator' };
    const c = { run_id: 'c', actor: 'investigator' };
    const trace = buildTrace([
      ev('claimed', { slot: 1, agent: 'investigator' }),
      ev('run_started', {}, a),
      ev('run_finished', { status: 'succeeded' }, a),
      ev('contract_failed', { error: 'missing evidence' }),
      ev('run_started', {}, retry),
      ev('released', { reason: 'shutdown' }),
      ev('claimed', { slot: 0, agent: 'investigator' }),
      ev('run_started', {}, c),
    ]);
    const runs = trace.entries.filter((e) => e.kind === 'run');
    expect(runs.map(runRole)).toEqual(['Investigator', 'Investigator', 'Investigator']);
    expect(runs.map((r) => r.slot)).toEqual([1, 1, 0]);
  });

  it('takes status, turns, cost, model and the version a run ran on from goald’s record of it', () => {
    const a = { run_id: 'a', actor: 'builder' };
    const trace = buildTrace(
      [
        ev('claimed', { slot: 0, agent: 'builder' }),
        ev('run_started', {}, a),
        ev('tool_call', { id: '1', tool: 'Read', input: '{}' }, a),
      ],
      [
        {
          id: 'a',
          item_id: 'item-1',
          station: 'build',
          agent: 'builder',
          model: 'claude-opus-5-5',
          status: 'failed',
          turns: 14,
          input_tokens: 900,
          output_tokens: 300,
          cost_usd: 1.12,
          error: 'max turns',
          started_at: '2026-10-23T14:02:00Z',
          ended_at: '2026-10-23T14:40:00Z',
          definition_version_id: 'v-9',
          definition_version: 9,
        },
      ],
    );
    expect(trace.entries[0]).toMatchObject({
      kind: 'run',
      status: 'failed',
      turns: 14,
      costUsd: 1.12,
      model: 'claude-opus-5-5',
      error: 'max turns',
      endedAt: '2026-10-23T14:40:00Z',
      version: 9,
    });
    expect(trace).toMatchObject({ costUsd: 1.12, turns: 14, model: 'claude-opus-5-5' });
    expect(activeRun(trace)).toBeUndefined();
  });

  it('leaves runs from before versions without one', () => {
    const a = { run_id: 'a', actor: 'builder' };
    const trace = buildTrace(
      [ev('run_started', {}, a)],
      [
        {
          id: 'a',
          item_id: 'item-1',
          station: 'build',
          agent: 'builder',
          model: 'claude-opus-5-5',
          status: 'running',
          turns: 0,
          input_tokens: 0,
          output_tokens: 0,
          cost_usd: 0,
          error: null,
          started_at: '2026-10-23T14:02:00Z',
          ended_at: null,
          definition_version_id: null,
          definition_version: null,
        },
      ],
    );
    expect(trace.entries[0]).toMatchObject({ kind: 'run', status: 'running' });
    expect(trace.entries[0]).not.toHaveProperty('version');
    expect(trace.entries[0]).not.toHaveProperty('turns');
    expect(runRole(trace.entries[0] as RunEntry)).toBe('Builder');
  });

  it('puts the factory rules a run ran with, by name and sha256, on that run', () => {
    const a = { run_id: 'a', actor: 'builder' };
    const trace = buildTrace([
      ev('run_started', {}, a),
      ev(
        'rules_applied',
        {
          rules: [
            { name: 'secrets', sha256: 'aaa' },
            { name: 'delivery', sha256: 'bbb' },
          ],
        },
        a,
      ),
    ]);
    expect(trace.entries[0]).toMatchObject({
      kind: 'run',
      rules: [
        { name: 'secrets', sha256: 'aaa' },
        { name: 'delivery', sha256: 'bbb' },
      ],
    });
  });

  it('leaves the rules off a run whose event says none applied', () => {
    const a = { run_id: 'a', actor: 'builder' };
    const trace = buildTrace([ev('run_started', {}, a), ev('rules_applied', { rules: [] }, a)]);
    expect(trace.entries[0]).not.toHaveProperty('rules');
  });

  it('notes a rule a review found broken, by the reviewing agent’s role, with the reason', () => {
    const trace = buildTrace([
      ev(
        'rule_violated',
        { rule: 'secrets', reason: 'Committed a token.', station: 'review', agent: 'reviewer' },
        {
          station: 'review',
          actor: 'reviewer',
        },
      ),
    ]);
    expect(trace.entries[0]).toMatchObject({
      kind: 'note',
      text: 'Reviewer found the rule secrets broken',
      detail: 'Committed a token.',
      tone: 'rework',
    });
  });
});

describe('toolArgument', () => {
  it('picks the telling argument of a JSON input and keeps truncated input readable', () => {
    expect(toolArgument('{"description":"x","command":"pnpm test"}')).toBe('pnpm test');
    expect(toolArgument('{"path":"cmd","pattern":"mux.Handle"}')).toBe('mux.Handle');
    expect(toolArgument('{"command":"go test ./... -run Tes')).toBe('{"command":"go test ./... -run Tes');
  });
});

describe('the agent reply in a run', () => {
  const run = { run_id: 'r9', actor: 'writer-1' };
  const replyOf = (text: string) =>
    (
      buildTrace([ev('run_started', {}, run), ev('agent_message', { text }, run)]).entries.find(
        (e) => e.kind === 'run',
      ) as RunEntry
    ).reply;

  it('reads a JSON reply as the output it is, never as a quoted JSON string', () => {
    expect(replyOf('{"summary":"Added /healthz","files":["main.go"]}')).toEqual({
      kind: 'output',
      value: { summary: 'Added /healthz', files: ['main.go'] },
    });
    expect(replyOf('Done.\n```json\n{"decision":"approve"}\n```')).toEqual({
      kind: 'output',
      value: { decision: 'approve' },
    });
  });

  it('keeps prose, and a string output, as text', () => {
    expect(replyOf('Looking at the router.')).toEqual({ kind: 'text', text: 'Looking at the router.' });
    expect(replyOf('"The notes are written."')).toEqual({ kind: 'text', text: 'The notes are written.' });
  });
});
