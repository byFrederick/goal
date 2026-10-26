// Turns an item's raw event history into the trace the Item screen shows: one block per agent
// run (its tool calls paired with their results, cost and turns), checks, and decisions. goald's
// record of each run (GET …/items/{item}/runs), when at hand, is the authority on its status,
// turns, cost and the definition version it ran on.
import type { ItemEvent, Run } from '../../api/types';
import { humanize } from '../../shared/format';
import type { Tone } from '../../shared/item-state';

export interface ToolCall {
  id: string;
  tool: string;
  /** The call's main argument (a path, a command, a pattern), shortened. */
  argument: string;
  summary?: string;
  isError: boolean;
  at: string;
}

export interface RunEntry {
  kind: 'run';
  key: string;
  /** The run's id. */
  id: string;
  at: string;
  endedAt?: string;
  station: string;
  agent: string;
  /** The station slot of the claim the run worked in, from 0: a machine field, never shown to people. */
  slot?: number;
  model?: string;
  /** The number of the definition version the run ran on. */
  version?: number;
  status: 'running' | 'succeeded' | 'failed' | 'cancelled';
  costUsd?: number;
  turns?: number;
  error?: string;
  tools: ToolCall[];
  /** The agent's last reply: prose, or the output it returned (its final reply is JSON). */
  reply?: Reply;
}

export type Reply = { kind: 'text'; text: string } | { kind: 'output'; value: unknown };

/**
 * What a reply says: an agent finishes by replying with its output, JSON (whole, or in a fenced
 * block, as goald reads it), so that is the output, not words to quote; a JSON string output is
 * text, and anything else is prose.
 */
export function replyOf(text: string): Reply {
  const fenced = /```(?:json)?\s*\n([\s\S]*?)\n?```/.exec(text)?.[1];
  for (const raw of [text.trim(), fenced?.trim()]) {
    if (!raw || !/^[[{"]/.test(raw)) continue;
    try {
      const value: unknown = JSON.parse(raw);
      return typeof value === 'string' ? { kind: 'text', text: value } : { kind: 'output', value };
    } catch {
      // not JSON: prose
    }
  }
  return { kind: 'text', text };
}

export interface CheckEntry {
  kind: 'check';
  key: string;
  at: string;
  station: string;
  command: string;
  passed: boolean;
  exitCode?: number;
  durationMs?: number;
}

export interface NoteEntry {
  kind: 'note';
  key: string;
  at: string;
  station: string;
  actor: string;
  text: string;
  detail?: string;
  tone: Tone;
}

export type Entry = RunEntry | CheckEntry | NoteEntry;

export interface Trace {
  entries: Entry[];
  costUsd: number;
  turns: number;
  model?: string;
}

const str = (v: unknown): string | undefined => (typeof v === 'string' && v !== '' ? v : undefined);
const num = (v: unknown): number | undefined => (typeof v === 'number' && Number.isFinite(v) ? v : undefined);

const argumentKeys = ['command', 'file_path', 'pattern', 'url', 'query', 'path', 'arg'];

/** The most telling argument of a tool call's (possibly truncated) JSON input. */
export function toolArgument(input: unknown): string {
  const raw = typeof input === 'string' ? input : JSON.stringify(input ?? '');
  let value: unknown = raw;
  try {
    value = JSON.parse(raw);
  } catch {
    // Cut to 500 characters by the worker; show it as it is.
  }
  if (value && typeof value === 'object' && !Array.isArray(value)) {
    const obj = value as Record<string, unknown>;
    const key =
      argumentKeys.find((k) => typeof obj[k] === 'string') ?? Object.keys(obj).find((k) => typeof obj[k] === 'string');
    if (key) value = obj[key];
  }
  const text = typeof value === 'string' ? value : JSON.stringify(value);
  return text.length > 140 ? `${text.slice(0, 139)}…` : text;
}

function note(e: ItemEvent): Omit<NoteEntry, 'kind' | 'key' | 'at' | 'station' | 'actor'> | undefined {
  const reason = str(e.data.reason);
  const withReason = (text: string, tone: Tone) => ({ text, tone, ...(reason ? { detail: reason } : {}) });
  const actor = e.actor;

  switch (e.kind) {
    case 'created':
      return { text: `Created in ${humanize(e.station ?? 'the first station')}`, tone: 'muted' };
    case 'moved':
      return { text: `Moved to ${humanize(str(e.data.to) ?? e.station ?? '')}`, tone: 'muted' };
    case 'claimed':
    case 'run_started':
    case 'agent_started':
    case 'usage':
      return undefined;
    case 'gate_approved':
      return { text: `${actor} passed it`, tone: 'ok' };
    case 'gate_rejected':
      return withReason(`${actor} sent it back`, 'rework');
    case 'approved':
      return { text: `${actor} approved`, tone: 'ok' };
    case 'rejected':
      return withReason(`${actor} sent it back`, 'rework');
    case 'note':
      return {
        text: `${actor} left a note for the next run`,
        tone: 'muted',
        ...(str(e.data.text) ? { detail: str(e.data.text) as string } : {}),
      };
    case 'merged':
      return { text: `${actor} merged the pull request`, tone: 'ok' };
    case 'retried':
      return { text: `${actor} retried it`, tone: 'run' };
    case 'waiting_human':
      return withReason('Waiting on you', 'you');
    case 'blocked':
      return withReason('Blocked', 'rework');
    case 'contract_failed':
      return {
        text: 'Output did not match the station contract',
        tone: 'rework',
        ...(str(e.data.error) ? { detail: str(e.data.error) as string } : {}),
      };
    case 'lease_expired':
      return { text: 'Lease expired, back in the queue', tone: 'muted' };
    case 'released':
      return withReason('Released back to the queue', 'muted');
    case 'done':
      return { text: 'Done', tone: 'ok' };
    default:
      return { text: humanize(e.kind), tone: 'muted' };
  }
}

/** A run's role as people read it: "Investigator" for the investigator agent. Its slot is a machine field, never shown. */
export function runRole(run: Pick<RunEntry, 'agent'>): string {
  return humanize(run.agent);
}

/** goald's record of a run over what its events said; turns and cost count once it ended. */
function applyRecord(run: RunEntry, record: Run) {
  run.status = record.status;
  run.model = record.model;
  if (record.status !== 'running') {
    run.turns = record.turns;
    run.costUsd = record.cost_usd;
  }
  if (record.ended_at) run.endedAt = record.ended_at;
  if (record.error) run.error = record.error;
  if (record.definition_version !== null) run.version = record.definition_version;
}

export function buildTrace(events: readonly ItemEvent[], records: readonly Run[] = []): Trace {
  const entries: Entry[] = [];
  const runs = new Map<string, RunEntry>();
  // The slot of the latest claim: every run until the next claim works in it.
  let slot: number | undefined;

  for (const e of events) {
    const station = e.station ?? '';

    if (e.kind === 'claimed') slot = num(e.data.slot);

    if (e.run_id) {
      let run = runs.get(e.run_id);
      if (!run) {
        run = {
          kind: 'run',
          key: `run-${e.run_id}`,
          id: e.run_id,
          at: e.at,
          station,
          agent: e.actor,
          ...(slot !== undefined ? { slot } : {}),
          status: 'running',
          tools: [],
        };
        runs.set(e.run_id, run);
        entries.push(run);
      }

      switch (e.kind) {
        case 'agent_started': {
          const m = str(e.data.model);
          if (m) run.model = m;
          break;
        }
        case 'tool_call':
          run.tools.push({
            id: str(e.data.id) ?? String(e.id),
            tool: str(e.data.tool) ?? 'tool',
            argument: toolArgument(e.data.input),
            isError: false,
            at: e.at,
          });
          break;
        case 'tool_result': {
          const id = str(e.data.id);
          const call = [...run.tools].reverse().find((t) => t.id === id && t.summary === undefined);
          if (call) {
            const summary = str(e.data.summary);
            if (summary) call.summary = summary;
            call.isError = e.data.is_error === true;
          }
          break;
        }
        case 'agent_message': {
          const text = str(e.data.text);
          if (text) run.reply = replyOf(text);
          break;
        }
        case 'run_finished': {
          const status = str(e.data.status);
          run.status = status === 'failed' || status === 'cancelled' ? status : 'succeeded';
          run.endedAt = e.at;
          const cost = num(e.data.cost_usd);
          const t = num(e.data.turns);
          const error = str(e.data.error);
          if (cost !== undefined) run.costUsd = cost;
          if (t !== undefined) run.turns = t;
          if (error) run.error = error;
          break;
        }
      }
      continue;
    }

    if (e.kind === 'check_passed' || e.kind === 'check_failed') {
      const exitCode = num(e.data.exit_code);
      const durationMs = num(e.data.duration_ms);
      entries.push({
        kind: 'check',
        key: `ev-${e.id}`,
        at: e.at,
        station,
        command: str(e.data.command) ?? 'check',
        passed: e.kind === 'check_passed',
        ...(exitCode !== undefined ? { exitCode } : {}),
        ...(durationMs !== undefined ? { durationMs } : {}),
      });
      continue;
    }

    const n = note(e);
    if (n) entries.push({ kind: 'note', key: `ev-${e.id}`, at: e.at, station, actor: e.actor, ...n });
  }

  for (const record of records) {
    const run = runs.get(record.id);
    if (run) applyRecord(run, record);
  }

  let costUsd = 0;
  let turns = 0;
  let model: string | undefined;
  for (const run of runs.values()) {
    costUsd += run.costUsd ?? 0;
    turns += run.turns ?? 0;
    model = run.model ?? model;
  }

  return { entries, costUsd, turns, ...(model ? { model } : {}) };
}

/** The last `n` tool calls across runs, newest last: the mobile "latest steps". */
export function latestToolCalls(trace: Trace, n: number): ToolCall[] {
  return trace.entries.flatMap((e) => (e.kind === 'run' ? e.tools : [])).slice(-n);
}

/** The run still working, if any. */
export function activeRun(trace: Trace): RunEntry | undefined {
  return trace.entries.findLast((e): e is RunEntry => e.kind === 'run' && e.status === 'running');
}

/** Whom a note reaches: the agent working the item, else the last one that worked at its station. */
export function noteRecipient(trace: Trace, station: string): string | undefined {
  const runs = trace.entries.filter((e): e is RunEntry => e.kind === 'run');
  return (activeRun(trace) ?? runs.findLast((r) => r.station === station))?.agent;
}
